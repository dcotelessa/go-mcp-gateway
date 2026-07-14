package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 15, cfg.IdleTimeoutMin)
	assert.Equal(t, 10, cfg.RequestTimeoutSec)
	assert.Equal(t, 30, cfg.InitTimeoutSec)

	go_, ok := cfg.ServerConfigs["go"]
	require.True(t, ok, "go server config must exist")
	assert.Equal(t, "gopls", go_.Command)

	ts, ok := cfg.ServerConfigs["typescript"]
	require.True(t, ok, "typescript server config must exist")
	assert.Equal(t, "typescript-language-server", ts.Command)
	assert.Contains(t, ts.Args, "--stdio")
}

func TestSessionKey(t *testing.T) {
	key := sessionKey("go", "/home/dcotelessa/dev/myproject")
	assert.Equal(t, "go|/home/dcotelessa/dev/myproject", key)

	// Different language same workspace = different key
	key2 := sessionKey("typescript", "/home/dcotelessa/dev/myproject")
	assert.NotEqual(t, key, key2)
}

func TestManager_New(t *testing.T) {
	m := New(DefaultConfig())
	defer m.Shutdown()
	assert.Equal(t, 0, m.SessionCount())
}

func TestManager_UnsupportedLanguage(t *testing.T) {
	m := New(DefaultConfig())
	defer m.Shutdown()

	_, err := m.GetOrCreate("rust", "/tmp/workspace")
	require.Error(t, err)

	var unsupported *ErrUnsupportedLanguage
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, "rust", unsupported.Language)
	assert.Contains(t, unsupported.Supported, "go")
	assert.Contains(t, unsupported.Supported, "typescript")
}

func TestManager_BinaryNotFound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServerConfigs["go"] = ServerConfig{
		Command:  "/nonexistent/gopls",
		Language: "go",
	}
	m := New(cfg)
	defer m.Shutdown()

	_, err := m.GetOrCreate("go", "/tmp/workspace")
	require.Error(t, err)

	var notFound *ErrBinaryNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "go", notFound.Language)
	assert.NotEmpty(t, notFound.InstallHint)
}

func TestErrorTypes(t *testing.T) {
	e1 := &ErrUnsupportedLanguage{Language: "rust", Supported: []string{"go", "typescript"}}
	assert.Contains(t, e1.Error(), "rust")
	assert.Contains(t, e1.Error(), "go")

	e2 := &ErrBinaryNotFound{Language: "go", ExpectedPath: "gopls", InstallHint: "go install ..."}
	assert.Contains(t, e2.Error(), "gopls")
	assert.Contains(t, e2.Error(), "go install")

	e3 := &ErrInitTimeout{Language: "go", WorkspaceRoot: "/tmp", TimeoutSec: 30}
	assert.Contains(t, e3.Error(), "30")

	e4 := &ErrRequestTimeout{Method: "textDocument/hover", TimeoutSec: 10}
	assert.Contains(t, e4.Error(), "hover")

	e5 := &ErrProcessCrashed{Language: "go", ExitCode: 1}
	assert.Contains(t, e5.Error(), "crashed")

	e6 := &ErrFileNotFound{Path: "/tmp/missing.go"}
	assert.Contains(t, e6.Error(), "missing.go")
}

func TestInstallHint(t *testing.T) {
	assert.Contains(t, installHint("go"), "gopls")
	assert.Contains(t, installHint("typescript"), "npm")
	assert.NotEmpty(t, installHint("unknown"))
}

func TestNewSession_Fields(t *testing.T) {
	cfg := DefaultConfig()
	sc := cfg.ServerConfigs["go"]
	s := newSession(sc, "/tmp/workspace", cfg)
	assert.NotNil(t, s.pending)
	assert.NotNil(t, s.openFiles)
	assert.NotNil(t, s.ready)
	assert.NotNil(t, s.crashed)
	assert.False(t, s.hasInFlight())
}
