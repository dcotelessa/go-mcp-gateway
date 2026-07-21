package modelmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dcotelessa/gateway/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// CompletionRequest is an OpenAI-compatible chat completion request.
type CompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

// ChatMessage is a single message in a completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionResponse is an OpenAI-compatible chat completion response.
type CompletionResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a single completion choice.
type Choice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage tracks token consumption for budget accounting.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

const completionTimeoutSec = 120

// Complete sends a completion request to the resident llama-server instance.
// Emits gen_ai.client.token.usage and gen_ai.client.operation.duration metrics.
func (m *Manager) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	r := m.Resident()
	if r == nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: no resident model")
	}

	// Start span and timer before the HTTP call
	tracer := otel.Tracer("github.com/dcotelessa/gateway")
	ctx, span := tracer.Start(ctx, "modelmanager.Complete")
	defer span.End()

	span.SetAttributes(
		attribute.String("gen_ai.system", telemetry.SystemForTier(r.Tier)),
		attribute.String("gen_ai.request.model", r.Tier),
		attribute.String("gateway.tier", r.Tier),
	)

	start := time.Now()

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", r.Port)

	body, err := json.Marshal(req)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.APIKey)

	client := &http.Client{Timeout: completionTimeoutSec * time.Second}
	resp, err := client.Do(httpReq)

	duration := time.Since(start).Seconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// Record duration even on error
		telemetry.RecordOperationDuration(ctx, telemetry.OpAttrs{
			System:    telemetry.SystemForTier(r.Tier),
			Model:     r.Tier,
			Tier:      r.Tier,
			Operation: "chat",
		}, duration)
		return CompletionResponse{}, fmt.Errorf("modelmanager: completion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("modelmanager: completion status %d", resp.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CompletionResponse{}, err
	}

	var result CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: decode response: %w", err)
	}

	// Record duration (success)
	telemetry.RecordOperationDuration(ctx, telemetry.OpAttrs{
		System:    telemetry.SystemForTier(r.Tier),
		Model:     r.Tier,
		Tier:      r.Tier,
		Operation: "chat",
	}, duration)

	// Record token usage (success only)
	attrs := telemetry.TokenUsageAttrs{
		System: telemetry.SystemForTier(r.Tier),
		Model:  r.Tier,
		Tier:   r.Tier,
	}
	telemetry.RecordTokenUsage(ctx, attrs, telemetry.TokenTypeInput, int64(result.Usage.PromptTokens))
	telemetry.RecordTokenUsage(ctx, attrs, telemetry.TokenTypeOutput, int64(result.Usage.CompletionTokens))

	span.SetAttributes(
		attribute.Int("gen_ai.usage.input_tokens", result.Usage.PromptTokens),
		attribute.Int("gen_ai.usage.output_tokens", result.Usage.CompletionTokens),
	)
	span.SetStatus(codes.Ok, "")

	return result, nil
}
