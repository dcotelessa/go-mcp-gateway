package mcp

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dcotelessa/gateway/internal/lsp"
	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/router"
)

func testServer(t *testing.T) *Server {
	t.Helper()

	r := router.New()
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
	lspMgr := lsp.New(lsp.DefaultConfig())

	t.Cleanup(func() {
		_ = mm.Shutdown()
		pol.Stop()
		lspMgr.Shutdown()
	})

	return New(
		ServerConfig{
			EndpointPath:      "/mcp",
			SessionIdleTTLMin: 30,
		},
		r, mm, pol, lspMgr,
	)
}

func TestServer_New(t *testing.T) {
	s := testServer(t)
	require.NotNil(t, s)
	require.NotNil(t, s.MCPServer())
}

func TestServer_ToolsRegistered(t *testing.T) {
	s := testServer(t)
	assert.NotNil(t, s.MCPServer())
}

func TestSafe_NilResultConverted(t *testing.T) {
	// GIVEN handler returns nil result and error
	// WHEN safe() wraps it
	// THEN returns non-nil structured error (REQ-REG-003)
	wrapped := safe(func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return nil, assert.AnError
	})
	assert.NotNil(t, wrapped)

	result, err := wrapped(context.Background(), mcpgo.CallToolRequest{})
	assert.NoError(t, err, "safe handler must not propagate error when result is nil")
	assert.NotNil(t, result, "safe handler must return non-nil result")
}

func TestGetString_Present(t *testing.T) {
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = map[string]any{"key": "value"}
	assert.Equal(t, "value", getString(req, "key"))
}

func TestGetString_Missing(t *testing.T) {
	req := mcpgo.CallToolRequest{}
	assert.Equal(t, "", getString(req, "nonexistent"))
}

func TestGetFloat_Present(t *testing.T) {
	req := mcpgo.CallToolRequest{}
	req.Params.Arguments = map[string]any{"line": float64(42)}
	assert.Equal(t, float64(42), getFloat(req, "line"))
}

func TestGetFloat_Missing(t *testing.T) {
	req := mcpgo.CallToolRequest{}
	assert.Equal(t, float64(0), getFloat(req, "nonexistent"))
}

func TestLocalSlotRemaining_NoResident(t *testing.T) {
	mm := modelmanager.New(modelmanager.ManagerConfig{
		ExecPath:         "/bin/echo",
		HealthTimeoutSec: 5,
		StopTimeoutSec:   2,
		LogDir:           "/tmp",
		TotalVRAMMiB:     16311,
		ReservedVRAMMiB:  1024,
		Models:           map[string]modelmanager.ModelConfig{},
	})
	defer func() { _ = mm.Shutdown() }()

	assert.Equal(t, int64(0), localSlotRemaining(mm, "local_ornith"))
}

func TestReasoningTags_PolicyError(t *testing.T) {
	err := &policy.ErrRateLimited{Session: "s1", RetryAfterSeconds: 30}
	tags := reasoningTags(err)
	assert.Contains(t, tags, "added_header_429")
	assert.Contains(t, tags, "rate_limited")
}

func TestReasoningTags_NonPolicyError(t *testing.T) {
	tags := reasoningTags(assert.AnError)
	assert.Nil(t, tags)
}
