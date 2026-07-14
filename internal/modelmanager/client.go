package modelmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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
func (m *Manager) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	r := m.Resident()
	if r == nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: no resident model")
	}

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
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: completion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("modelmanager: completion status %d", resp.StatusCode)
	}

	var result CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return CompletionResponse{}, fmt.Errorf("modelmanager: decode response: %w", err)
	}

	return result, nil
}
