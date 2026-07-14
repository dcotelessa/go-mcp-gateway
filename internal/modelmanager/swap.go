package modelmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// EnsureLoaded guarantees the requested tier's model is resident in VRAM.
// If a different model is resident, it queues a swap request.
// If the requested model is already resident, returns immediately (fast path).
func (m *Manager) EnsureLoaded(ctx context.Context, tier string) (*ResidentState, error) {
	// Fast path — already resident
	r := m.Resident()
	if r != nil && r.Tier == tier && !r.Swapping {
		return r, nil
	}

	// Queue a swap request — the swapProcessor serializes these
	result := make(chan swapResult, 1)
	select {
	case m.swapQueue <- swapRequest{tier: tier, result: result}:
	default:
		return nil, fmt.Errorf("modelmanager: swap queue full — swap in progress")
	}

	select {
	case res := <-result:
		return res.state, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// doSwap executes the full swap sequence:
// SIGTERM → wait → SIGKILL → VRAM drain → start new → health poll → mark resident
func (m *Manager) doSwap(ctx context.Context, tier string) (*ResidentState, error) {
	model, ok := m.cfg.Models[tier]
	if !ok {
		return nil, fmt.Errorf("modelmanager: unknown tier %q", tier)
	}

	// Check VRAM feasibility
	budget, err := ComputeBudget(m.cfg.TotalVRAMMiB, m.cfg.ReservedVRAMMiB, model.VRAMRequirementMiB)
	if err != nil {
		return nil, err
	}

	// Mark swapping
	m.mu.Lock()
	if m.resident != nil {
		m.resident.Swapping = true
	}
	m.mu.Unlock()

	// Stop current resident if any
	if r := m.Resident(); r != nil {
		if err := m.killProcess(r.PID, m.cfg.StopTimeoutSec); err != nil {
			return nil, fmt.Errorf("modelmanager: stop resident: %w", err)
		}
		m.setResident(nil)
		// Brief drain to let VRAM release
		time.Sleep(500 * time.Millisecond)
	}

	// Apply layer split args if needed
	modelWithSplit := model
	if budget.NeedsLayerSplit {
		modelWithSplit.ExtraArgs = append(LayerSplitArgs(budget), model.ExtraArgs...)
	}

	// Generate internal API key
	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("modelmanager: generate api key: %w", err)
	}

	// Start new process
	cmd, err := m.startProcess(tier, modelWithSplit, apiKey)
	if err != nil {
		return nil, fmt.Errorf("modelmanager: %w", err)
	}

	// Health poll
	port := model.Port
	if err := m.pollHealth(ctx, port, m.cfg.HealthTimeoutSec); err != nil {
		_ = m.killProcess(cmd.Process.Pid, 5)
		m.setResident(nil)
		return nil, fmt.Errorf("modelmanager: health check failed for %s: %w", tier, err)
	}

	// Mark resident
	state := &ResidentState{
		Tier:      tier,
		ModelPath: model.Path,
		Port:      port,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		APIKey:    apiKey,
	}
	m.setResident(state)

	// Watch for unexpected process death
	go m.watchProcess(cmd, tier)

	copy := *state
	return &copy, nil
}

// pollHealth polls GET /health on the given port until 200 or timeout.
func (m *Manager) pollHealth(ctx context.Context, port, timeoutSec int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("health check timeout after %ds", timeoutSec)
}

// watchProcess monitors a running llama-server and clears resident state on exit.
func (m *Manager) watchProcess(cmd interface{ Wait() error }, tier string) {
	_ = cmd.Wait()
	m.mu.Lock()
	if m.resident != nil && m.resident.Tier == tier {
		m.resident = nil
	}
	m.mu.Unlock()
}

// generateAPIKey generates a cryptographically random 32-byte hex string.
func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
