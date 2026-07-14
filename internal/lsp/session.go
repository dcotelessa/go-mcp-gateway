package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Session represents a single LSP server process for one workspace.
type Session struct {
	sc            ServerConfig
	workspaceRoot string
	cfg           Config

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu       sync.Mutex
	pending  map[int64]chan jsonRPCResponse
	openFiles map[string]string // path → content hash

	nextID    int64
	inFlight  int64 // atomic counter
	lastUsed  int64 // atomic unix nano

	ready     chan struct{}
	readyOnce sync.Once
	readyErr  error

	crashed chan struct{}
}

// jsonRPCRequest is a minimal JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a minimal JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the error object in a JSON-RPC response.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// newSession creates a Session (does not start the process).
func newSession(sc ServerConfig, workspaceRoot string, cfg Config) *Session {
	return &Session{
		sc:            sc,
		workspaceRoot: workspaceRoot,
		cfg:           cfg,
		pending:       make(map[int64]chan jsonRPCResponse),
		openFiles:     make(map[string]string),
		ready:         make(chan struct{}),
		crashed:       make(chan struct{}),
	}
}

// start launches the LSP binary and performs initialization.
func (s *Session) start() error {
	// Verify binary exists
	path, err := exec.LookPath(s.sc.Command)
	if err != nil {
		return &ErrBinaryNotFound{
			Language:     s.sc.Language,
			ExpectedPath: s.sc.Command,
			InstallHint:  installHint(s.sc.Language),
		}
	}

	args := append([]string{path}, s.sc.Args...)
	s.cmd = exec.Command(args[0], args[1:]...)
	s.cmd.Env = os.Environ()

	s.stdin, err = s.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("lsp: stdin pipe: %w", err)
	}

	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("lsp: stdout pipe: %w", err)
	}
	s.stdout = bufio.NewReader(stdout)

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("lsp: start %s: %w", s.sc.Command, err)
	}

	// Start reader goroutine
	go s.readLoop()

	// Initialize asynchronously
	go s.initialize()

	return nil
}

// initialize sends initialize + initialized and marks session ready.
func (s *Session) initialize() {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(s.cfg.InitTimeoutSec)*time.Second,
	)
	defer cancel()

	params := map[string]interface{}{
		"rootUri": "file://" + s.workspaceRoot,
		"workspaceFolders": []map[string]string{
			{"uri": "file://" + s.workspaceRoot, "name": s.workspaceRoot},
		},
		"capabilities": map[string]interface{}{},
	}

	_, err := s.request(ctx, "initialize", params)
	if err != nil {
		s.readyOnce.Do(func() { s.readyErr = err; close(s.ready) })
		return
	}

	// Send initialized notification (no response expected)
	_ = s.notify("initialized", map[string]interface{}{})

	s.readyOnce.Do(func() { close(s.ready) })
}

// waitReady blocks until the session is initialized or times out.
func (s *Session) waitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return s.readyErr
	case <-ctx.Done():
		return &ErrInitTimeout{
			Language:      s.sc.Language,
			WorkspaceRoot: s.workspaceRoot,
			TimeoutSec:    s.cfg.InitTimeoutSec,
		}
	case <-s.crashed:
		return &ErrProcessCrashed{Language: s.sc.Language}
	}
}

// Request sends a JSON-RPC request and waits for the response.
func (s *Session) Request(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	atomic.AddInt64(&s.inFlight, 1)
	defer atomic.AddInt64(&s.inFlight, -1)
	atomic.StoreInt64(&s.lastUsed, time.Now().UnixNano())

	// Ensure initialized
	readyCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.InitTimeoutSec)*time.Second)
	defer cancel()
	if err := s.waitReady(readyCtx); err != nil {
		return nil, err
	}

	return s.request(ctx, method, params)
}

// request sends a JSON-RPC request and waits for its response (internal).
func (s *Session) request(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&s.nextID, 1)

	ch := make(chan jsonRPCResponse, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	if err := s.send(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return nil, err
	}

	timeout := time.Duration(s.cfg.RequestTimeoutSec) * time.Second
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp: %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		return nil, &ErrRequestTimeout{Method: method, TimeoutSec: s.cfg.RequestTimeoutSec}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.crashed:
		return nil, &ErrProcessCrashed{Language: s.sc.Language}
	}
}

// notify sends a JSON-RPC notification (no ID, no response expected).
func (s *Session) notify(method string, params interface{}) error {
	return s.send(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

// send writes a JSON-RPC message to stdin with Content-Length framing.
func (s *Session) send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("lsp: marshal: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.stdin, header); err != nil {
		return err
	}
	_, err = s.stdin.Write(data)
	return err
}

// readLoop reads JSON-RPC messages from stdout and routes them.
func (s *Session) readLoop() {
	for {
		msg, err := s.readMessage()
		if err != nil {
			close(s.crashed)
			return
		}

		// Route to pending channel if it has an ID
		if msg.ID != 0 {
			s.mu.Lock()
			ch, ok := s.pending[msg.ID]
			s.mu.Unlock()
			if ok {
				ch <- msg
			}
		}
		// Notifications (no ID) are dropped for now — diagnostics handled separately
	}
}

// readMessage reads one Content-Length framed JSON-RPC message from stdout.
func (s *Session) readMessage() (jsonRPCResponse, error) {
	var contentLength int

	// Read headers
	for {
		line, err := s.stdout.ReadString('\n')
		if err != nil {
			return jsonRPCResponse{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				contentLength, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		}
	}

	if contentLength == 0 {
		return jsonRPCResponse{}, fmt.Errorf("lsp: missing Content-Length")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.stdout, body); err != nil {
		return jsonRPCResponse{}, err
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return jsonRPCResponse{}, err
	}
	return resp, nil
}

// idleSince returns when the session was last used.
func (s *Session) idleSince() time.Time {
	nano := atomic.LoadInt64(&s.lastUsed)
	if nano == 0 {
		return time.Unix(0, 0) // never used — treat as epoch (always idle)
	}
	return time.Unix(0, nano)
}

// hasInFlight returns true if there are in-flight requests.
func (s *Session) hasInFlight() bool {
	return atomic.LoadInt64(&s.inFlight) > 0
}

// wait blocks until the process exits (used by watchSession).
func (s *Session) wait() {
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
}

// shutdown sends shutdown + exit then waits for process to end.
func (s *Session) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.request(ctx, "shutdown", nil)
	_ = s.notify("exit", nil)
	_ = s.stdin.Close()
	return nil
}

// kill force-kills the process.
func (s *Session) kill() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// installHint returns a human-readable install suggestion for a language binary.
func installHint(language string) string {
	hints := map[string]string{
		"go":         "run: go install golang.org/x/tools/gopls@latest",
		"typescript": "run: npm install -g typescript-language-server typescript",
	}
	if h, ok := hints[language]; ok {
		return h
	}
	return "check your PATH"
}

// EnsureFileOpen sends textDocument/didOpen if the file hasn't been opened yet,
// or didChange if the content has changed since last open.
func (s *Session) EnsureFileOpen(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return &ErrFileNotFound{Path: path}
	}
	content := string(data)

	s.mu.Lock()
	prev, alreadyOpen := s.openFiles[path]
	s.openFiles[path] = content
	s.mu.Unlock()

	uri := "file://" + path

	if !alreadyOpen {
		return s.notify("textDocument/didOpen", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        uri,
				"languageId": s.sc.Language,
				"version":    1,
				"text":       content,
			},
		})
	}

	if prev != content {
		return s.notify("textDocument/didChange", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     uri,
				"version": 2,
			},
			"contentChanges": []map[string]interface{}{
				{"text": content},
			},
		})
	}

	return nil
}
