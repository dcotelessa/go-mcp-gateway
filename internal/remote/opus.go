package remote

import (
	"context"
	"fmt"
	"net/http"
)

const (
	opusName    = "claude_opus"
	opusBaseURL = "https://openrouter.ai/api/v1/chat/completions"
	opusModel   = "anthropic/claude-opus-4.8"
)

// OpusAdapter routes to Claude Opus 4.8 via OpenRouter.
// Uses effort level "high" — never "max" to control token spend.
// Requires OPENROUTER_API_KEY environment variable.
type OpusAdapter struct {
	client *baseClient
}

// NewOpusAdapter creates a Claude Opus 4.8 adapter via OpenRouter.
func NewOpusAdapter(apiKey string) (*OpusAdapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("remote: Opus adapter requires OPENROUTER_API_KEY")
	}
	return &OpusAdapter{
		client: newBaseClient(
			opusName,
			opusBaseURL,
			apiKey,
			opusModel,
			map[string]string{
				"HTTP-Referer": "https://github.com/dcotelessa/go-mcp-gateway",
				"X-Title":     "go-mcp-gateway",
			},
		),
	}, nil
}

// Do sends a completion request to Claude Opus 4.8 via OpenRouter.
// Sets reasoning effort to "high" — the ceiling for this gateway.
func (a *OpusAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	return doWithRetryAndHeader(
		context.Background(),
		func(ctx context.Context) (RemoteResult, int, string, error) {
			// Inject effort level via system prompt convention
			// OpenRouter maps reasoning.effort in the request body
			req.SystemPrompt = withEffortHint(req.SystemPrompt, "high")
			result, status, err := a.client.do(ctx, req)
			retryAfter := ""
			if status == http.StatusTooManyRequests {
				retryAfter = ""
			}
			return result, status, retryAfter, err
		},
		opusName,
	)
}

// Name returns the provider name.
func (a *OpusAdapter) Name() string { return opusName }

// withEffortHint appends an effort instruction to the system prompt.
// OpenRouter's reasoning.effort parameter requires request body injection
// which the base client doesn't support yet — system prompt is the fallback.
func withEffortHint(systemPrompt, effort string) string {
	hint := fmt.Sprintf("[reasoning_effort:%s]", effort)
	if systemPrompt == "" {
		return hint
	}
	return systemPrompt + "\n" + hint
}
