package lsp

import (
	"fmt"
	"sync"
	"time"
)

// Manager owns the pool of LSP sessions keyed by workspace path.
// Sessions are independent of MCP session lifecycle.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	cfg      Config
	done     chan struct{}
}

// Config holds LSP manager configuration.
type Config struct {
	IdleTimeoutMin    int
	RequestTimeoutSec int
	InitTimeoutSec    int
	// ServerConfigs maps language name to binary config
	ServerConfigs map[string]ServerConfig
}

// ServerConfig describes how to launch an LSP binary.
type ServerConfig struct {
	Command  string   // e.g. "gopls", "typescript-language-server"
	Args     []string // e.g. ["--stdio"]
	Language string
}

// DefaultConfig returns sensible defaults with go and typescript configured.
func DefaultConfig() Config {
	return Config{
		IdleTimeoutMin:    15,
		RequestTimeoutSec: 10,
		InitTimeoutSec:    30,
		ServerConfigs: map[string]ServerConfig{
			"go": {
				Command:  "gopls",
				Args:     []string{},
				Language: "go",
			},
			"typescript": {
				Command:  "typescript-language-server",
				Args:     []string{"--stdio"},
				Language: "typescript",
			},
		},
	}
}

// sessionKey returns the map key for a language+workspace combination.
// Decoupled from MCP session ID by design (REQ-LSP-001, REQ-LSP-002).
func sessionKey(language, workspaceRoot string) string {
	return language + "|" + workspaceRoot
}

// New creates a Manager and starts the background idle sweep.
func New(cfg Config) *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
		cfg:      cfg,
		done:     make(chan struct{}),
	}
	go m.sweepLoop()
	return m
}

// GetOrCreate returns an existing session for the workspace or starts a new one.
// Returns ErrUnsupportedLanguage if the language has no configured binary.
// Returns ErrBinaryNotFound if the binary is not installed.
func (m *Manager) GetOrCreate(language, workspaceRoot string) (*Session, error) {
	sc, ok := m.cfg.ServerConfigs[language]
	if !ok {
		return nil, &ErrUnsupportedLanguage{
			Language:  language,
			Supported: m.supportedLanguages(),
		}
	}

	key := sessionKey(language, workspaceRoot)

	m.mu.Lock()
	if s, ok := m.sessions[key]; ok {
		m.mu.Unlock()
		return s, nil
	}

	s := newSession(sc, workspaceRoot, m.cfg)
	m.sessions[key] = s
	m.mu.Unlock()

	if err := s.start(); err != nil {
		m.mu.Lock()
		delete(m.sessions, key)
		m.mu.Unlock()
		return nil, err
	}

	// Watch for unexpected process death
	go m.watchSession(key, s)

	return s, nil
}

// watchSession monitors a session and removes it from the pool on exit.
func (m *Manager) watchSession(key string, s *Session) {
	s.wait()
	m.mu.Lock()
	if m.sessions[key] == s {
		delete(m.sessions, key)
	}
	m.mu.Unlock()
}

// Close explicitly closes a session and removes it from the pool.
func (m *Manager) Close(language, workspaceRoot string) error {
	key := sessionKey(language, workspaceRoot)
	m.mu.Lock()
	s, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}
	return s.shutdown()
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Shutdown closes all sessions gracefully then force-kills any remaining.
func (m *Manager) Shutdown() {
	close(m.done)

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			_ = s.shutdown()
		}(s)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Force kill any remaining
		for _, s := range sessions {
			s.kill()
		}
	}
}

// sweepLoop periodically evicts idle sessions.
func (m *Manager) sweepLoop() {
	ticker := time.NewTicker(time.Duration(m.cfg.IdleTimeoutMin) * time.Minute / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.sweep()
		case <-m.done:
			return
		}
	}
}

// sweep evicts sessions idle beyond the configured timeout.
func (m *Manager) sweep() {
	cutoff := time.Now().Add(-time.Duration(m.cfg.IdleTimeoutMin) * time.Minute)

	m.mu.Lock()
	var toClose []*Session
	for key, s := range m.sessions {
		if s.idleSince().Before(cutoff) && !s.hasInFlight() {
			toClose = append(toClose, s)
			delete(m.sessions, key)
		}
	}
	m.mu.Unlock()

	for _, s := range toClose {
		_ = s.shutdown()
	}
}

// supportedLanguages returns the list of configured language names.
func (m *Manager) supportedLanguages() []string {
	langs := make([]string, 0, len(m.cfg.ServerConfigs))
	for lang := range m.cfg.ServerConfigs {
		langs = append(langs, lang)
	}
	return langs
}

// LookupResult is returned by diagnostic/hover/definition queries.
type LookupResult struct {
	Raw []byte // raw JSON-RPC result payload
}

// DiagnosticsResult holds file diagnostics from publishDiagnostics.
type DiagnosticsResult struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Diagnostic is a single LSP diagnostic item.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// Range is an LSP range with start and end positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position is an LSP position (line/character, zero-based).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// fmt import used for sessionKey — keep compiler happy
var _ = fmt.Sprintf
