package remote

import (
	"context"
	"fmt"
	"net/http"
)

const (
	glmName    = "z_ai"
	glmBaseURL = "https://api.z.ai/api/coding/v1/chat/completions"
	glmModel   = "glm-5.2"
)

// GLMAdapter routes to GLM-5.2 via the Z.ai Coding Plan endpoint.
// Requires ZAI_API_KEY (or ZHIPU_API_KEY) environment variable.
type GLMAdapter struct {
	client *baseClient
}

// NewGLMAdapter creates a GLM-5.2 adapter using the Z.ai Coding Plan API key.
func NewGLMAdapter(apiKey string) (*GLMAdapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("remote: GLM adapter requires ZAI_API_KEY")
	}
	return &GLMAdapter{
		client: newBaseClient(
			glmName,
			glmBaseURL,
			apiKey,
			glmModel,
			nil, // no extra headers for Z.ai
		),
	}, nil
}

// Do sends a completion request to GLM-5.2 via Z.ai Coding Plan.
func (a *GLMAdapter) Do(req RemoteRequest) (RemoteResult, error) {
	return doWithRetryAndHeader(
		context.Background(),
		func(ctx context.Context) (RemoteResult, int, string, error) {
			result, status, err := a.client.do(ctx, req)
			retryAfter := ""
			if status == http.StatusTooManyRequests {
				retryAfter = ""
			}
			return result, status, retryAfter, err
		},
		glmName,
	)
}

// Name returns the provider name.
func (a *GLMAdapter) Name() string { return glmName }
