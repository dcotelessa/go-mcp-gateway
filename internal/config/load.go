package config

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads configuration from (in priority order):
//  1. -config CLI flag
//  2. GATEWAY_CONFIG environment variable
//  3. ./config.yaml if it exists
//  4. embedded defaults
//
// Env vars are applied on top of the loaded file.
func Load() (Config, error) {
	cfg := Defaults()

	path := resolveConfigPath()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	applyEnvOverrides(&cfg)

	return cfg, nil
}

// resolveConfigPath returns the first config file path that should be loaded,
// or empty string if none found.
func resolveConfigPath() string {
	// 1. -config flag (only parsed if not already parsed)
	configFlag := flag.Lookup("config")
	if configFlag != nil && configFlag.Value.String() != "" {
		return configFlag.Value.String()
	}

	// 2. GATEWAY_CONFIG env var
	if p := os.Getenv("GATEWAY_CONFIG"); p != "" {
		return p
	}

	// 3. ./config.yaml if present
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}

	return ""
}

// applyEnvOverrides applies environment variable overrides on top of cfg.
func applyEnvOverrides(cfg *Config) {
	// Remote API keys
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		cfg.RemoteAPIs.DeepSeekAPIKey = v
	}
	if v := os.Getenv("GLM_API_KEY"); v != "" {
		cfg.RemoteAPIs.GLMAPIKey = v
	}

	// llama-server path
	if v := os.Getenv("LLAMA_SERVER_PATH"); v != "" {
		cfg.LlamaServer.ExecPath = v
	}

	// Policy budget overrides
	if v := envInt("BUDGET_DEEPSEEK_DAILY"); v > 0 {
		cfg.Policy.BudgetDeepSeekDaily = v
	}
	if v := envInt("BUDGET_GLM_DAILY"); v > 0 {
		cfg.Policy.BudgetGLMDaily = v
	}
	if v := envInt("SESSION_RATE_PER_MIN"); v > 0 {
		cfg.Policy.SessionRatePerMin = v
	}
	if v := envInt("SESSION_TOKENS_PER_HOUR"); v > 0 {
		cfg.Policy.SessionTokensPerHour = v
	}
	if v := envInt("SESSION_IDLE_TTL_MIN"); v > 0 {
		cfg.Policy.SessionIdleTTLMin = v
	}
	if v := envInt("BUDGET_SWEEP_INTERVAL_MIN"); v > 0 {
		cfg.Policy.BudgetSweepIntervalMin = v
	}
}

// envInt reads an env var as int, returns 0 if missing or unparseable.
func envInt(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0
	}
	return n
}
