// Package remote provides OpenAI-compatible HTTP clients for remote LLM tiers.
// It handles 429 retry logic, provider-specific adapters, and OpenRouter fallback.
//
// Tier → adapter mapping:
//   remote_deepseek → DeepSeek V4-Flash via OpenRouter ($0.14/M input)
//   remote_glm      → GLM-5.2 via Z.ai Coding Plan endpoint
//
// Fallback: on TerminalError, OpenRouter fallback adapter fires once.
// On RateLimitedError (429 exhausted), fallback does NOT fire — cascade
// is handled by the policy layer, not here.
package remote

import (
	"fmt"
	"time"
)

// RemoteRequest is the input to a remote completion call.
type RemoteRequest struct {
	// Task is the user message content sent to the model.
	Task string

	// MaxTokens caps the response length. 0 = provider default.
	MaxTokens int

	// Stream requests SSE streaming. Tokens are aggregated before return.
	Stream bool

	// Tier identifies the routing tier for logging and telemetry.
	Tier string

	// Complexity is the task complexity for telemetry attributes.
	Complexity string

	// SystemPrompt is an optional system message prepended to the request.
	SystemPrompt string
}

// RemoteResult is the output of a successful remote completion call.
type RemoteResult struct {
	// Content is the model's response text.
	Content string

	// PromptTokens is the number of input tokens consumed.
	PromptTokens int

	// CompletionTokens is the number of output tokens generated.
	CompletionTokens int

	// Model is the model identifier returned by the provider.
	Model string

	// Provider identifies which adapter produced this result.
	Provider string
}

// TotalTokens returns the sum of prompt and completion tokens.
func (r RemoteResult) TotalTokens() int {
	return r.PromptTokens + r.CompletionTokens
}

// RateLimitedError is returned when 429 retries are exhausted.
// The policy layer uses this to trigger the cascade to the next tier.
// Fallback adapters do NOT fire on this error.
type RateLimitedError struct {
	Provider       string
	RetryAfterSecs int
	Attempts       int
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("remote: %s rate limited after %d attempts (retry after %ds)",
		e.Provider, e.Attempts, e.RetryAfterSecs)
}

// TerminalError is returned on non-retryable failures (5xx, parse errors, etc).
// The fallback adapter fires on this error type.
type TerminalError struct {
	Provider   string
	StatusCode int
	Message    string
}

func (e *TerminalError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("remote: %s terminal error %d: %s",
			e.Provider, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("remote: %s terminal error: %s", e.Provider, e.Message)
}

// Adapter is the interface all provider adapters implement.
type Adapter interface {
	// Do sends a completion request and returns the result.
	// Implementations handle their own 429 retry logic internally.
	Do(req RemoteRequest) (RemoteResult, error)

	// Name returns the provider name for logging and telemetry.
	Name() string
}

// openAIRequest is the wire format sent to OpenAI-compatible endpoints.
type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
}

// openAIMessage is a single message in the OpenAI chat format.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse is the non-streaming response from OpenAI-compatible endpoints.
type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// openAIStreamChunk is a single SSE chunk in a streaming response.
type openAIStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// retryState tracks retry attempts for 429 handling.
type retryState struct {
	attempts   int
	maxRetries int
	nextWait   time.Duration
}
