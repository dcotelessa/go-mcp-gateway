package remote

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	opusName    = "claude_opus"
	opusBaseURL = "https://openrouter.ai/api/v1/chat/completions"
	opusModel   = "anthropic/claude-opus-5"
)

// OpusAdapter routes to Claude Opus 5 via OpenRouter.
// Uses effort level "high" — never above for cost control.
// Opus 5 default is high; five levels available: low/medium/high/xhigh/max.
// Requires OPENROUTER_API_KEY environment variable.
type OpusAdapter struct {
	client *baseClient
}

// NewOpusAdapter creates a Claude Opus 5 adapter via OpenRouter.
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

// Do sends a completion request to Claude Opus 5 via OpenRouter.
// Opus gets 180s timeout — it thinks longer at high effort.
func (a *OpusAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	return doWithRetryAndHeader(
		ctx,
		func(ctx context.Context) (RemoteResult, int, string, error) {
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
func withEffortHint(systemPrompt, effort string) string {
	hint := fmt.Sprintf("[reasoning_effort:%s]", effort)
	if systemPrompt == "" {
		return hint
	}
	return systemPrompt + "\n" + hint
}
