package config

import (
	"errors"
	"fmt"
	"os"
)

// Validate checks that the config is safe to run with.
// Returns an error describing the first fatal problem found.
func Validate(cfg Config) error {
	// llama-server exec must exist and be executable
	if cfg.LlamaServer.ExecPath == "" {
		return errors.New("config: llama_server.exec_path is required (or set LLAMA_SERVER_PATH)")
	}
	if _, err := os.Stat(cfg.LlamaServer.ExecPath); err != nil {
		return fmt.Errorf("config: llama_server.exec_path %q not found: %w", cfg.LlamaServer.ExecPath, err)
	}

	// Each model file must exist on disk
	for tier, model := range cfg.Models {
		if model.Path == "" {
			return fmt.Errorf("config: model %q has no path", tier)
		}
		if _, err := os.Stat(model.Path); err != nil {
			return fmt.Errorf("config: model %q path %q not found: %w", tier, model.Path, err)
		}
	}

	// Remote tiers must have API keys if configured
	if cfg.RemoteAPIs.DeepSeekBaseURL != "" && cfg.RemoteAPIs.DeepSeekAPIKey == "" {
		return errors.New("config: remote_apis.deepseek_base_url set but DEEPSEEK_API_KEY missing")
	}
	if cfg.RemoteAPIs.GLMBaseURL != "" && cfg.RemoteAPIs.GLMAPIKey == "" {
		return errors.New("config: remote_apis.glm_base_url set but GLM_API_KEY missing")
	}

	return nil
}
