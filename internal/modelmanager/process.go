package modelmanager

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// startProcess launches llama-server for the given model config on the given port.
// Returns the started *exec.Cmd or an error.
func (m *Manager) startProcess(tier string, model ModelConfig, apiKey string) (*exec.Cmd, error) {
	args := buildArgs(model, apiKey)

	cmd := exec.Command(m.cfg.ExecPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Redirect stdout/stderr to log file
	logPath := fmt.Sprintf("%s/llama-%s.log", m.cfg.LogDir, tier)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("modelmanager: start %s: %w", tier, err)
	}
	return cmd, nil
}

// buildArgs constructs the llama-server command line arguments.
func buildArgs(model ModelConfig, apiKey string) []string {
	args := []string{
		"--model", model.Path,
		"--port", fmt.Sprintf("%d", model.Port),
		"--alias", model.Path,
		"--api-key", apiKey,
	}
	args = append(args, model.ExtraArgs...)
	return args
}

// killProcess sends SIGTERM then waits up to stopTimeoutSec before SIGKILL.
func (m *Manager) killProcess(pid, stopTimeoutSec int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil // already gone
	}

	_ = proc.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case <-done:
		return nil
	case <-time.After(time.Duration(stopTimeoutSec) * time.Second):
		_ = proc.Signal(syscall.SIGKILL)
		return nil
	}
}
