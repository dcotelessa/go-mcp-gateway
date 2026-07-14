package modelmanager

import (
	"context"
	"sync"
	"time"
)

// Manager owns the lifecycle of local llama-server processes.
// Only one model may be resident in VRAM at a time.
type Manager struct {
	mu        sync.RWMutex
	resident  *ResidentState
	swapQueue chan swapRequest
	done      chan struct{}
	cfg       ManagerConfig
}

// ManagerConfig holds the runtime configuration for the Manager.
type ManagerConfig struct {
	ExecPath         string
	HealthTimeoutSec int
	StopTimeoutSec   int
	LogDir           string
	TotalVRAMMiB     int
	ReservedVRAMMiB  int
	Models           map[string]ModelConfig
}

// ModelConfig describes a single model the manager can load.
type ModelConfig struct {
	Path               string
	VRAMRequirementMiB int
	ExtraArgs          []string
	Port               int
}

// ResidentState describes the currently-loaded model process.
type ResidentState struct {
	Tier      string
	ModelPath string
	Port      int
	PID       int
	StartedAt time.Time
	APIKey    string   // internal key, never exposed outside gateway
	Swapping  bool     // true while a swap is in progress
}

// swapRequest is sent to the swap queue goroutine.
type swapRequest struct {
	tier   string
	result chan swapResult
}

// swapResult is the response from the swap queue goroutine.
type swapResult struct {
	state *ResidentState
	err   error
}

// New creates a Manager and starts the background swap processor.
func New(cfg ManagerConfig) *Manager {
	m := &Manager{
		swapQueue: make(chan swapRequest, 1),
		done:      make(chan struct{}),
		cfg:       cfg,
	}
	go m.swapProcessor()
	return m
}

// Resident returns a copy of the current resident state (nil if none loaded).
func (m *Manager) Resident() *ResidentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.resident == nil {
		return nil
	}
	copy := *m.resident
	return &copy
}

// setResident replaces the resident state under write lock.
func (m *Manager) setResident(s *ResidentState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resident = s
}

// swapProcessor is the single goroutine that serializes all model swaps.
func (m *Manager) swapProcessor() {
	for {
		select {
		case req := <-m.swapQueue:
			state, err := m.doSwap(context.Background(), req.tier)
			req.result <- swapResult{state: state, err: err}
		case <-m.done:
			return
		}
	}
}

// Shutdown stops the resident model and the swap processor.
func (m *Manager) Shutdown() error {
	close(m.done)
	r := m.Resident()
	if r == nil {
		return nil
	}
	return m.killProcess(r.PID, m.cfg.StopTimeoutSec)
}
