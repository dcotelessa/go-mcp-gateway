package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/router"
)

func testMux(t *testing.T) *http.ServeMux {
	t.Helper()

	mm := modelmanager.New(modelmanager.ManagerConfig{
		ExecPath:         "/bin/echo",
		HealthTimeoutSec: 5,
		StopTimeoutSec:   2,
		LogDir:           "/tmp",
		TotalVRAMMiB:     16311,
		ReservedVRAMMiB:  1024,
		Models:           map[string]modelmanager.ModelConfig{},
	})
	pol := policy.New(policy.DefaultPolicyConfig())

	t.Cleanup(func() {
		_ = mm.Shutdown()
		pol.Stop()
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, HandlerConfig{
		Router:   router.New(),
		Manager:  mm,
		Policy:   pol,
		DrainSec: 10,
	})
	return mux
}

func postJSON(t *testing.T, mux *http.ServeMux, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// --- /classify ---

func TestClassify_Success(t *testing.T) {
	mux := testMux(t)
	rr := postJSON(t, mux, "/classify", ClassifyRequest{Task: "scaffold the config package"})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp ClassifyResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.NotEmpty(t, resp.Complexity)
	assert.NotEmpty(t, resp.QALevel)
}

func TestClassify_MissingTask(t *testing.T) {
	mux := testMux(t)
	rr := postJSON(t, mux, "/classify", ClassifyRequest{Task: ""})

	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "missing_task", resp.Error)
}

func TestClassify_WrongMethod(t *testing.T) {
	mux := testMux(t)
	req := httptest.NewRequest(http.MethodGet, "/classify", nil)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	assert.Equal(t, "POST", rr.Header().Get("Allow"))
}

func TestClassify_WrongContentType(t *testing.T) {
	mux := testMux(t)
	req := httptest.NewRequest(http.MethodPost, "/classify",
		bytes.NewReader([]byte(`{"task":"test"}`)))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, rr.Code)
}

// --- /interpret ---

func TestInterpret_Pass(t *testing.T) {
	mux := testMux(t)
	rr := postJSON(t, mux, "/interpret", InterpretRequest{
		VitestOutput: "All tests passed (5/5)",
		Diff:         "",
		Scenarios:    []string{},
	})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp InterpretResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "pass", resp.Status)
	assert.Equal(t, "merge", resp.NextAction)
}

func TestInterpret_Fail(t *testing.T) {
	mux := testMux(t)
	rr := postJSON(t, mux, "/interpret", InterpretRequest{
		VitestOutput: "× router_test.go:42 expected ornith got qwen\nfailed 1 test",
		Diff:         "",
		Scenarios:    []string{},
	})

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp InterpretResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "fail", resp.Status)
	assert.Equal(t, "retry", resp.NextAction)
}

func TestInterpret_WrongMethod(t *testing.T) {
	mux := testMux(t)
	req := httptest.NewRequest(http.MethodGet, "/interpret", nil)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

// --- /implement ---

func TestImplement_Success(t *testing.T) {
	mux := testMux(t)
	rr := postJSON(t, mux, "/implement", ImplementRequest{
		Task:          "scaffold the config package",
		Files:         []string{"internal/config/config.go"},
		ReasoningTags: []string{},
	})

	// Scaffold tasks route to local_ornith — no model loaded, expect 503
	// This validates the route path executes without panic
	assert.True(t, rr.Code == http.StatusOK || rr.Code == http.StatusServiceUnavailable,
		"expected 200 or 503 depending on model availability, got %d", rr.Code)
}

func TestImplement_RateLimit(t *testing.T) {
	mm := modelmanager.New(modelmanager.ManagerConfig{
		ExecPath:         "/bin/echo",
		HealthTimeoutSec: 5,
		StopTimeoutSec:   2,
		LogDir:           "/tmp",
		TotalVRAMMiB:     16311,
		ReservedVRAMMiB:  1024,
		Models:           map[string]modelmanager.ModelConfig{},
	})
	cfg := policy.PolicyConfig{
		RateLimitPerMin:     1, // very low limit
		TokenLimitPerHour:   200000,
		SessionIdleTTLMin:   30,
		SweepIntervalMin:    60,
		BudgetDeepSeekDaily: 1000000,
		BudgetGLMDaily:      500000,
	}
	pol := policy.New(cfg)
	t.Cleanup(func() {
		_ = mm.Shutdown()
		pol.Stop()
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, HandlerConfig{
		Router:   router.New(),
		Manager:  mm,
		Policy:   pol,
		DrainSec: 10,
	})

	// First request consumes the rate limit
	postJSON(t, mux, "/implement", ImplementRequest{Task: "scaffold"})

	// Second request should be rate limited
	rr := postJSON(t, mux, "/implement", ImplementRequest{Task: "scaffold"})
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "rate_limited", resp.Error)
}

// --- unknown route ---

func TestUnknownRoute_404(t *testing.T) {
	mux := testMux(t)
	req := httptest.NewRequest(http.MethodGet, "/unknown/path", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "not_found", resp.Error)
}

// --- error envelope ---

func TestErrorEnvelope_Consistent(t *testing.T) {
	mux := testMux(t)

	// Any error response must have {error, detail} shape
	rr := postJSON(t, mux, "/classify", ClassifyRequest{Task: ""})
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.NotEmpty(t, resp.Error, "error field must be present")
}

// --- session synthesis ---

func TestSessionKey_XSessionId(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/implement", nil)
	req.Header.Set("X-Session-Id", "my-session-123")
	assert.Equal(t, "my-session-123", sessionKey(req))
}

func TestSessionKey_Authorization(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/implement", nil)
	req.Header.Set("Authorization", "Bearer token123")
	key := sessionKey(req)
	assert.True(t, len(key) > 0)
	assert.Contains(t, key, "rest-auth-")
}

func TestSessionKey_Anonymous(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/implement", nil)
	assert.Equal(t, "rest-anonymous", sessionKey(req))
}

// --- models ---

func TestModels_DTOFields(t *testing.T) {
	// Verify DTO field names match Mastra contract
	resp := ImplementResponse{
		FilesChanged:  []string{"main.go"},
		Status:        "complete",
		ReasoningTags: []string{"forced_tier"},
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "files_changed")
	assert.Contains(t, m, "status")
	assert.Contains(t, m, "reasoning_tags")
}
