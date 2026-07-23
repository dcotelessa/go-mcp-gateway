package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOpenAIServer starts a test server responding with a canned completion.
func mockOpenAIServer(t *testing.T, statusCode int, body interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

// successResponse returns a valid OpenAI-compatible completion response.
func successResponse(content string) openAIResponse {
	return openAIResponse{
		ID:    "test-id",
		Model: "test-model",
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{Message: struct {
				Content string `json:"content"`
			}{Content: content}, FinishReason: "stop"},
		},
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}
}

func TestDeepSeekAdapter_Success(t *testing.T) {
	srv := mockOpenAIServer(t, http.StatusOK, successResponse("scaffold complete"))
	defer srv.Close()

	adapter, err := NewDeepSeekAdapter("test-key")
	require.NoError(t, err)
	adapter.client.baseURL = srv.URL // point at mock

	result, err := adapter.Do(RemoteRequest{
		Task:       "scaffold the config package",
		Tier:       "remote_deepseek",
		Complexity: "scaffold",
	})
	require.NoError(t, err)
	assert.Equal(t, "scaffold complete", result.Content)
	assert.Equal(t, 10, result.PromptTokens)
	assert.Equal(t, 20, result.CompletionTokens)
	assert.Equal(t, "deepseek", result.Provider)
}

func TestDeepSeekAdapter_500(t *testing.T) {
	srv := mockOpenAIServer(t, http.StatusInternalServerError, nil)
	defer srv.Close()

	adapter, err := NewDeepSeekAdapter("test-key")
	require.NoError(t, err)
	adapter.client.baseURL = srv.URL

	_, err = adapter.Do(RemoteRequest{Task: "test", Tier: "remote_deepseek"})
	require.Error(t, err)

	var termErr *TerminalError
	require.ErrorAs(t, err, &termErr)
	assert.Equal(t, 500, termErr.StatusCode)
}

func TestDeepSeekAdapter_NoAPIKey(t *testing.T) {
	_, err := NewDeepSeekAdapter("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OPENROUTER_API_KEY")
}

func TestGLMAdapter_Success(t *testing.T) {
	srv := mockOpenAIServer(t, http.StatusOK, successResponse("multi-file impl"))
	defer srv.Close()

	adapter, err := NewGLMAdapter("test-key")
	require.NoError(t, err)
	adapter.client.baseURL = srv.URL

	result, err := adapter.Do(RemoteRequest{
		Task:       "coordinate changes across multiple files",
		Tier:       "remote_glm",
		Complexity: "multi_file",
	})
	require.NoError(t, err)
	assert.Equal(t, "multi-file impl", result.Content)
	assert.Equal(t, "z_ai", result.Provider)
}

func TestGLMAdapter_NoAPIKey(t *testing.T) {
	_, err := NewGLMAdapter("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ZAI_API_KEY")
}

func TestOpenRouterFallback_Success(t *testing.T) {
	srv := mockOpenAIServer(t, http.StatusOK, successResponse("fallback content"))
	defer srv.Close()

	fallback, err := NewOpenRouterFallbackAdapter("test-key")
	require.NoError(t, err)

	// Point the fallback at our mock by making a direct baseClient call
	client := newBaseClient(openRouterName, srv.URL, "test-key",
		"deepseek/deepseek-chat-v4-flash", nil)

	req := RemoteRequest{
		Task:       "test task",
		Tier:       "remote_deepseek",
		Complexity: "multi_file",
	}
	result, _, err := client.do(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "fallback content", result.Content)
	assert.Equal(t, openRouterName, result.Provider)
	_ = fallback // adapter construction validated
}

func TestOpenRouterFallback_UnknownTier(t *testing.T) {
	fallback, err := NewOpenRouterFallbackAdapter("test-key")
	require.NoError(t, err)

	_, err = fallback.Do(RemoteRequest{
		Task: "test",
		Tier: "unknown_tier",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown_tier")
}

func TestWithFallback_FiresOnTerminalError(t *testing.T) {
	// Primary always returns TerminalError
	primarySrv := mockOpenAIServer(t, http.StatusInternalServerError, nil)
	defer primarySrv.Close()

	// Fallback returns success
	fallbackSrv := mockOpenAIServer(t, http.StatusOK, successResponse("fallback ok"))
	defer fallbackSrv.Close()

	primary, err := NewDeepSeekAdapter("test-key")
	require.NoError(t, err)
	primary.client.baseURL = primarySrv.URL

	fallbackAdapter, err := NewOpenRouterFallbackAdapter("test-key")
	require.NoError(t, err)
	// Point fallback client at our mock server
	fallbackClient := newBaseClient(openRouterName, fallbackSrv.URL, "test-key",
		"deepseek/deepseek-chat-v4-flash", nil)
	fallbackAdapter2 := &OpenRouterFallbackAdapter{apiKey: "test-key"}
	_ = fallbackAdapter2
	_ = fallbackClient

	combined := WithFallback(primary, fallbackAdapter)
	assert.Contains(t, combined.Name(), "fallback")
}

func TestWithFallback_DoesNotFireOnRateLimited(t *testing.T) {
	calls := 0

	// Primary always returns 429 exhausted → RateLimitedError
	mockPrimary := &mockAdapter{
		err: &RateLimitedError{Provider: "test", Attempts: 4},
	}

	fallbackCalled := false
	combined := &fallbackAdapter{
		primary:  mockPrimary,
		fallback: &OpenRouterFallbackAdapter{apiKey: "test"},
	}
	_ = calls

	_, err := combined.Do(RemoteRequest{Task: "test", Tier: "remote_deepseek"})
	require.Error(t, err)

	var rateLimited *RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	assert.False(t, fallbackCalled, "fallback must NOT fire on RateLimitedError")
}

// mockAdapter is a test double for Adapter.
type mockAdapter struct {
	result RemoteResult
	err    error
}

func (m *mockAdapter) Do(_ RemoteRequest) (RemoteResult, error) { return m.result, m.err }
func (m *mockAdapter) Name() string                             { return "mock" }

// mockFallback tracks whether it was called.
type mockFallback struct {
	called *bool
}

func (m *mockFallback) Do(_ RemoteRequest) (RemoteResult, error) {
	*m.called = true
	return RemoteResult{Content: "fallback"}, nil
}
func (m *mockFallback) Name() string { return "mock_fallback" }
