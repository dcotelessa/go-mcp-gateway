package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTimeoutSec = 120

// baseClient is an OpenAI-compatible HTTP client shared by all adapters.
type baseClient struct {
	name     string
	baseURL  string
	apiKey   string
	model    string
	timeout  time.Duration
	headers  map[string]string // extra headers per provider
}

// newBaseClient creates a configured base client.
func newBaseClient(name, baseURL, apiKey, model string, extraHeaders map[string]string) *baseClient {
	return &baseClient{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		timeout: defaultTimeoutSec * time.Second,
		headers: extraHeaders,
	}
}

// do sends a single completion request — no retry logic, no fallback.
// Callers wrap this with retry logic as needed.
func (c *baseClient) do(ctx context.Context, req RemoteRequest) (RemoteResult, int, error) {
	if req.Stream {
		return c.doStream(ctx, req)
	}
	return c.doSync(ctx, req)
}

// doSync sends a non-streaming request.
func (c *baseClient) doSync(ctx context.Context, req RemoteRequest) (RemoteResult, int, error) {
	messages := buildMessages(req)

	payload := openAIRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}
	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "marshal: " + err.Error()}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "build request: " + err.Error()}
	}

	c.setHeaders(httpReq)

	client := &http.Client{Timeout: c.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "http: " + err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return RemoteResult{}, resp.StatusCode, nil // caller handles 429
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return RemoteResult{}, resp.StatusCode, &TerminalError{
			Provider:   c.name,
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
		}
	}

	var apiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return RemoteResult{}, resp.StatusCode, &TerminalError{
			Provider: c.name,
			Message:  "decode: " + err.Error(),
		}
	}

	if len(apiResp.Choices) == 0 {
		return RemoteResult{}, resp.StatusCode, &TerminalError{
			Provider: c.name,
			Message:  "no choices in response",
		}
	}

	return RemoteResult{
		Content:          apiResp.Choices[0].Message.Content,
		PromptTokens:     apiResp.Usage.PromptTokens,
		CompletionTokens: apiResp.Usage.CompletionTokens,
		Model:            apiResp.Model,
		Provider:         c.name,
	}, resp.StatusCode, nil
}

// setHeaders applies auth and provider-specific headers.
func (c *baseClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
}

// buildMessages constructs the messages array from the request.
func buildMessages(req RemoteRequest) []openAIMessage {
	var messages []openAIMessage
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}
	messages = append(messages, openAIMessage{
		Role:    "user",
		Content: req.Task,
	})
	return messages
}

// Name returns the provider name.
func (c *baseClient) Name() string {
	return c.name
}

// suppress unused fmt warning
var _ = fmt.Sprintf
