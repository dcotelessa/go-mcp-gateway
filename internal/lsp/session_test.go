package lsp

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_Initialize(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assert.NoError(t, s.waitReady(ctx))
}

func TestSession_Request_AfterInit(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := s.Request(ctx, "shutdown", nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestSession_Request_Timeout(t *testing.T) {
	// GIVEN session with 1s request timeout
	// WHEN unknown/method sent (mock won't respond)
	// THEN returns error
	bin := mockLSPBinary(t)
	cfg := DefaultConfig()
	cfg.RequestTimeoutSec = 1
	cfg.InitTimeoutSec = 10
	sc := ServerConfig{Command: bin, Args: []string{}, Language: "go"}

	s := newSession(sc, t.TempDir(), cfg)

	var err error
	s.cmd = exec.Command(bin)
	s.cmd.Env = os.Environ()
	s.stdin, err = s.cmd.StdinPipe()
	require.NoError(t, err)

	stdout, err := s.cmd.StdoutPipe()
	require.NoError(t, err)
	s.stdout = bufio.NewReader(stdout)

	require.NoError(t, s.cmd.Start())
	t.Cleanup(func() { s.kill(); _ = s.cmd.Wait() })

	go s.readLoop()
	go s.initialize()

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()
	require.NoError(t, s.waitReady(initCtx))

	// unknown/method gets no response — hits 1s timeout
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reqCancel()
	_, err = s.request(reqCtx, "unknown/method", nil)
	assert.Error(t, err, "expected error for unrecognized method")
}

func TestSession_ConcurrentRequests(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.waitReady(ctx))

	var wg sync.WaitGroup
	errors := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer reqCancel()
			_, errors[idx] = s.Request(reqCtx, "shutdown", nil)
		}(i)
	}
	wg.Wait()

	for _, err := range errors {
		if err != nil {
			assert.IsType(t, &ErrRequestTimeout{}, err)
		}
	}
}

func TestSession_EnsureFileOpen_NewFile(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.waitReady(ctx))

	tmp := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(tmp, []byte("package main"), 0644))

	err := s.EnsureFileOpen(tmp)
	assert.NoError(t, err)

	s.mu.Lock()
	_, tracked := s.openFiles[tmp]
	s.mu.Unlock()
	assert.True(t, tracked)
}

func TestSession_EnsureFileOpen_AlreadyOpen_NoChange(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.waitReady(ctx))

	tmp := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(tmp, []byte("package main"), 0644))
	require.NoError(t, s.EnsureFileOpen(tmp))

	err := s.EnsureFileOpen(tmp)
	assert.NoError(t, err)
}

func TestSession_EnsureFileOpen_ContentChanged(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.waitReady(ctx))

	tmp := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(tmp, []byte("package main"), 0644))
	require.NoError(t, s.EnsureFileOpen(tmp))

	require.NoError(t, os.WriteFile(tmp, []byte("package main\n\nfunc main() {}"), 0644))
	err := s.EnsureFileOpen(tmp)
	assert.NoError(t, err)
}

func TestSession_EnsureFileOpen_Missing(t *testing.T) {
	s := sessionWithMock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, s.waitReady(ctx))

	err := s.EnsureFileOpen("/nonexistent/path/main.go")
	require.Error(t, err)

	var notFound *ErrFileNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Contains(t, notFound.Path, "main.go")
}

func TestSession_IdleTracking(t *testing.T) {
	s := sessionWithMock(t)
	assert.False(t, s.hasInFlight())
	assert.False(t, s.idleSince().IsZero())
}

func TestManager_SessionReuse(t *testing.T) {
	bin := mockLSPBinary(t)
	cfg := DefaultConfig()
	cfg.ServerConfigs["go"] = ServerConfig{
		Command:  bin,
		Args:     []string{},
		Language: "go",
	}
	m := New(cfg)
	defer m.Shutdown()

	workspace := t.TempDir()
	s1, err := m.GetOrCreate("go", workspace)
	require.NoError(t, err)

	s2, err := m.GetOrCreate("go", workspace)
	require.NoError(t, err)

	assert.Same(t, s1, s2)
	assert.Equal(t, 1, m.SessionCount())
}

func TestManager_DifferentWorkspaces(t *testing.T) {
	bin := mockLSPBinary(t)
	cfg := DefaultConfig()
	cfg.ServerConfigs["go"] = ServerConfig{
		Command:  bin,
		Args:     []string{},
		Language: "go",
	}
	m := New(cfg)
	defer m.Shutdown()

	s1, err := m.GetOrCreate("go", t.TempDir())
	require.NoError(t, err)

	s2, err := m.GetOrCreate("go", t.TempDir())
	require.NoError(t, err)

	assert.NotSame(t, s1, s2)
	assert.Equal(t, 2, m.SessionCount())
}

func TestManager_Sweep_EvictsIdle(t *testing.T) {
	bin := mockLSPBinary(t)
	cfg := DefaultConfig()
	cfg.IdleTimeoutMin = 0
	cfg.ServerConfigs["go"] = ServerConfig{
		Command:  bin,
		Language: "go",
	}
	m := New(cfg)
	defer m.Shutdown()

	_, err := m.GetOrCreate("go", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 1, m.SessionCount())

	m.sweep()
	assert.Equal(t, 0, m.SessionCount())
}

func TestManager_Close(t *testing.T) {
	bin := mockLSPBinary(t)
	cfg := DefaultConfig()
	cfg.ServerConfigs["go"] = ServerConfig{
		Command:  bin,
		Language: "go",
	}
	m := New(cfg)
	defer m.Shutdown()

	workspace := t.TempDir()
	_, err := m.GetOrCreate("go", workspace)
	require.NoError(t, err)
	assert.Equal(t, 1, m.SessionCount())

	err = m.Close("go", workspace)
	assert.NoError(t, err)
	assert.Equal(t, 0, m.SessionCount())
}
