package modelmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLlamaServer starts a test HTTP server that responds to /health and
// /v1/chat/completions. Returns the server and its port.
func mockLlamaServer(t *testing.T, healthy bool) (*httptest.Server, int) {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "test-id",
			"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	parts := strings.Split(srv.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])
	return srv, port
}

func TestEnsureLoaded_FastPath(t *testing.T) {
	// GIVEN model B already resident and not swapping
	// WHEN EnsureLoaded called for same tier
	// THEN returns immediately without queuing swap
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	// Pre-populate resident state
	m.setResident(&ResidentState{
		Tier:      "local_ornith",
		ModelPath: "/tmp/ornith.gguf",
		Port:      8081,
		PID:       99999,
		StartedAt: time.Now(),
		Swapping:  false,
	})

	start := time.Now()
	r, err := m.EnsureLoaded(context.Background(), "local_ornith")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "local_ornith", r.Tier)
	assert.Less(t, elapsed, 50*time.Millisecond, "fast path should return immediately")
}

func TestEnsureLoaded_UnknownTier(t *testing.T) {
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	_, err := m.EnsureLoaded(context.Background(), "nonexistent_tier")
	assert.Error(t, err)
}

func TestResidentState_ConcurrentReadWrite(t *testing.T) {
	// GIVEN concurrent goroutines reading and writing ResidentState
	// THEN no data races
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.setResident(&ResidentState{Tier: "local_ornith"})
		}()
		go func() {
			defer wg.Done()
			_ = m.Resident()
		}()
	}
	wg.Wait()
}

func TestPollHealth_Success(t *testing.T) {
	// GIVEN mock server returns 200 on /health
	// WHEN pollHealth called
	// THEN returns nil immediately
	_, port := mockLlamaServer(t, true)

	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	err := m.pollHealth(context.Background(), port, 5)
	assert.NoError(t, err)
}

func TestPollHealth_Timeout(t *testing.T) {
	// GIVEN no server listening on port
	// WHEN pollHealth called with short timeout
	// THEN returns timeout error
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	err := m.pollHealth(context.Background(), 19999, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestPollHealth_ContextCancelled(t *testing.T) {
	// GIVEN context cancelled before health check completes
	// WHEN pollHealth called
	// THEN returns context error
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := m.pollHealth(ctx, 19999, 10)
	assert.Error(t, err)
}

func TestComplete_NoResident(t *testing.T) {
	// GIVEN no resident model
	// WHEN Complete called
	// THEN returns error
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	_, err := m.Complete(context.Background(), CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no resident model")
}

func TestComplete_WithResident(t *testing.T) {
	// GIVEN mock llama-server running
	// WHEN Complete called with resident pointing at mock
	// THEN returns parsed completion response
	_, port := mockLlamaServer(t, true)

	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	m.setResident(&ResidentState{
		Tier:   "local_ornith",
		Port:   port,
		APIKey: "test-key",
	})

	resp, err := m.Complete(context.Background(), CompletionRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 15, resp.Usage.TotalTokens)
	assert.Equal(t, "ok", resp.Choices[0].Message.Content)
}

func TestWatchProcess_ClearsResidentOnExit(t *testing.T) {
	// GIVEN resident state set for local_ornith
	// WHEN watchProcess detects process exit
	// THEN clears resident state
	m := New(testConfig())
	defer func() { _ = m.Shutdown() }()

	m.setResident(&ResidentState{Tier: "local_ornith"})
	require.NotNil(t, m.Resident())

	// Use a cmd that exits immediately
	done := make(chan error, 1)
	done <- nil

	mockCmd := &mockCmd{done: done}
	go m.watchProcess(mockCmd, "local_ornith")

	// Wait for watchProcess to clear state
	assert.Eventually(t, func() bool {
		return m.Resident() == nil
	}, time.Second, 10*time.Millisecond)
}

// mockCmd satisfies the interface{ Wait() error } used by watchProcess.
type mockCmd struct {
	done chan error
}

func (c *mockCmd) Wait() error {
	return <-c.done
}

func TestGenerateAPIKey_Unique(t *testing.T) {
	// GIVEN two separate key generations
	// THEN keys are unique and 64 hex chars each
	keys := make(map[string]bool)
	for i := 0; i < 10; i++ {
		key, err := generateAPIKey()
		require.NoError(t, err)
		assert.Len(t, key, 64)
		assert.False(t, keys[key], "key must be unique")
		keys[key] = true
	}
}
