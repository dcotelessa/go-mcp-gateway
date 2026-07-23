package remote

import (
	"context"
	"fmt"
	"net/http"
)

const (
	deepSeekName    = "deepseek"
	deepSeekBaseURL = "https://openrouter.ai/api/v1/chat/completions"
	deepSeekModel   = "deepseek/deepseek-v4-flash"
)

// DeepSeekAdapter routes to DeepSeek V4-Flash via OpenRouter.
// Requires OPENROUTER_API_KEY environment variable.
type DeepSeekAdapter struct {
	client *baseClient
}

// NewDeepSeekAdapter creates a DeepSeek adapter using the OpenRouter API key.
func NewDeepSeekAdapter(apiKey string) (*DeepSeekAdapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("remote: DeepSeek adapter requires OPENROUTER_API_KEY")
	}
	return &DeepSeekAdapter{
		client: newBaseClient(
			deepSeekName,
			deepSeekBaseURL,
			apiKey,
			deepSeekModel,
			map[string]string{
				"HTTP-Referer": "https://github.com/dcotelessa/go-mcp-gateway",
				"X-Title":     "go-mcp-gateway",
			},
		),
	}, nil
}

// Do sends a completion request to DeepSeek V4-Flash via OpenRouter.
func (a *DeepSeekAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	return doWithRetryAndHeader(
		context.Background(),
		func(ctx context.Context) (RemoteResult, int, string, error) {
			result, status, err := a.client.do(ctx, req)
			retryAfter := ""
			if status == http.StatusTooManyRequests {
				// Header not available here — exponential backoff handles it
				retryAfter = ""
			}
			return result, status, retryAfter, err
		},
		deepSeekName,
	)
}

// Name returns the provider name.
func (a *DeepSeekAdapter) Name() string { return deepSeekName }
