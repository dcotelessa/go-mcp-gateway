package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const (
	openRouterName    = "openrouter_fallback"
	openRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"
)

// tierToOpenRouterModel maps gateway tiers to OpenRouter model aliases.
var tierToOpenRouterModel = map[string]string{
	"remote_deepseek": "deepseek/deepseek-v4-flash",
	"remote_glm":      "z-ai/glm-5.2",
}

// OpenRouterFallbackAdapter fires on TerminalError from a primary adapter.
// Does NOT fire on RateLimitedError — that triggers the policy cascade.
// Does NOT recurse into itself.
type OpenRouterFallbackAdapter struct {
	apiKey string
}

// NewOpenRouterFallbackAdapter creates a fallback adapter.
func NewOpenRouterFallbackAdapter(apiKey string) (*OpenRouterFallbackAdapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("remote: OpenRouter fallback requires OPENROUTER_API_KEY")
	}
	return &OpenRouterFallbackAdapter{apiKey: apiKey}, nil
}

// Do sends a completion via OpenRouter using the tier-mapped model alias.
// Only fires for TerminalError from the primary adapter — never for RateLimitedError.
func (a *OpenRouterFallbackAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	model, ok := tierToOpenRouterModel[req.Tier]
	if !ok {
		return RemoteResult{}, &TerminalError{
			Provider: openRouterName,
			Message:  fmt.Sprintf("no OpenRouter model mapping for tier %q", req.Tier),
		}
	}

	client := newBaseClient(
		openRouterName,
		openRouterBaseURL,
		a.apiKey,
		model,
		map[string]string{
			"HTTP-Referer": "https://github.com/dcotelessa/go-mcp-gateway",
			"X-Title":     "go-mcp-gateway-fallback",
		},
	)

	return doWithRetryAndHeader(
		context.Background(),
		func(ctx context.Context) (RemoteResult, int, string, error) {
			result, status, err := client.do(ctx, req)
			return result, status, "", err
		},
		openRouterName,
	)
}

// Name returns the provider name.
func (a *OpenRouterFallbackAdapter) Name() string { return openRouterName }

// WithFallback wraps a primary adapter with OpenRouter fallback.
// Fallback fires only on TerminalError, not RateLimitedError.
func WithFallback(primary Adapter, fallback *OpenRouterFallbackAdapter) Adapter {
	return &fallbackAdapter{primary: primary, fallback: fallback}
}

type fallbackAdapter struct {
	primary  Adapter
	fallback *OpenRouterFallbackAdapter
}

func (a *fallbackAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	result, err := a.primary.Do(req)
	if err == nil {
		return result, nil
	}

	// Only fall back on TerminalError — not RateLimitedError
	var termErr *TerminalError
	if !errors.As(err, &termErr) {
		return RemoteResult{}, err
	}

	return a.fallback.Do(req)
}

func (a *fallbackAdapter) Name() string {
	return fmt.Sprintf("%s+fallback", a.primary.Name())
}

// suppress unused import
var _ = http.StatusOK
