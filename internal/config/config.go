package config

// Config is the root configuration object loaded from config.yaml.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	MCP        MCPConfig        `yaml:"mcp"`
	LlamaServer LlamaServerConfig `yaml:"llama_server"`
	VRAM       VRAMConfig       `yaml:"vram"`
	Models     map[string]ModelConfig `yaml:"models"`
	RemoteAPIs RemoteAPIConfig  `yaml:"remote_apis"`
	Policy     PolicyConfig     `yaml:"policy"`
	LSP        LSPConfig        `yaml:"lsp"`
	REST       RESTConfig       `yaml:"rest"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"`
}

type ServerConfig struct {
	Port int `yaml:"port"` // default 8080
}

type MCPConfig struct {
	SessionIdleTTLMinutes int    `yaml:"session_idle_ttl_minutes"` // default 30
	EndpointPath          string `yaml:"endpoint_path"`            // default /mcp
}

type LlamaServerConfig struct {
	ExecPath        string `yaml:"exec_path"`
	HealthTimeoutSec int   `yaml:"health_timeout_sec"` // default 60
	StopTimeoutSec  int   `yaml:"stop_timeout_sec"`   // default 10
	LogDir          string `yaml:"log_dir"`            // default ./logs
}

type VRAMConfig struct {
	TotalMiB     int `yaml:"total_mib"`     // 16311
	ReservedMiB  int `yaml:"reserved_mib"`  // default 1024
}

type ModelConfig struct {
	Path           string   `yaml:"path"`
	VRAMRequirementMiB int  `yaml:"vram_requirement_mib"`
	ExtraArgs      []string `yaml:"extra_args"`
	Port           int      `yaml:"port"`
}

type RemoteAPIConfig struct {
	DeepSeekAPIKey  string `yaml:"deepseek_api_key"`
	DeepSeekBaseURL string `yaml:"deepseek_base_url"`
	GLMAPIKey       string `yaml:"glm_api_key"`
	GLMBaseURL      string `yaml:"glm_base_url"`
}

type PolicyConfig struct {
	BudgetDeepSeekDaily    int `yaml:"budget_deepseek_daily"`
	BudgetGLMDaily         int `yaml:"budget_glm_daily"`
	SessionRatePerMin      int `yaml:"session_rate_per_min"`
	SessionTokensPerHour   int `yaml:"session_tokens_per_hour"`
	SessionIdleTTLMin      int `yaml:"session_idle_ttl_min"`
	BudgetSweepIntervalMin int `yaml:"budget_sweep_interval_min"`
}

type LSPConfig struct {
	IdleTimeoutMin     int `yaml:"idle_timeout_min"`      // default 15
	RequestTimeoutSec  int `yaml:"request_timeout_sec"`   // default 10
	InitTimeoutSec     int `yaml:"init_timeout_sec"`      // default 30
}

type RESTConfig struct {
	ShutdownDrainSec int `yaml:"shutdown_drain_sec"` // default 10
}

// Defaults returns a Config populated with sensible defaults.
func Defaults() Config {
	return Config{
		Server: ServerConfig{Port: 8080},
		MCP: MCPConfig{
			SessionIdleTTLMinutes: 30,
			EndpointPath:          "/mcp",
		},
		LlamaServer: LlamaServerConfig{
			HealthTimeoutSec: 60,
			StopTimeoutSec:   10,
			LogDir:           "./logs",
		},
		VRAM: VRAMConfig{
			TotalMiB:    16311,
			ReservedMiB: 1024,
		},
		Policy: PolicyConfig{
			BudgetDeepSeekDaily:    1000000,
			BudgetGLMDaily:         500000,
			SessionRatePerMin:      20,
			SessionTokensPerHour:   200000,
			SessionIdleTTLMin:      30,
			BudgetSweepIntervalMin: 5,
		},
		LSP: LSPConfig{
			IdleTimeoutMin:    15,
			RequestTimeoutSec: 10,
			InitTimeoutSec:    30,
		},
		REST: RESTConfig{
			ShutdownDrainSec: 10,
		},
		Telemetry: TelemetryConfig{
			Enabled: false,
			Service: TelemetryServiceConfig{
				Name:    "go-mcp-gateway",
				Version: "0.2.0",
			},
			OTLP: TelemetryOTLPConfig{
				Endpoint: "http://localhost:4318",
				Insecure: true,
			},
			Metrics: TelemetryMetricsConfig{ExportIntervalSec: 15},
			Traces:  TelemetryTracesConfig{SamplingRatio: 1.0},
		},
	}
}

// TelemetryConfig holds OpenTelemetry configuration.
type TelemetryConfig struct {
	Enabled bool                    `yaml:"enabled"`
	Service TelemetryServiceConfig  `yaml:"service"`
	OTLP    TelemetryOTLPConfig     `yaml:"otlp"`
	Metrics TelemetryMetricsConfig  `yaml:"metrics"`
	Traces  TelemetryTracesConfig   `yaml:"traces"`
}

// TelemetryServiceConfig identifies the service in telemetry backends.
type TelemetryServiceConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// TelemetryOTLPConfig configures the OTLP HTTP exporter.
type TelemetryOTLPConfig struct {
	Endpoint string `yaml:"endpoint"`
	Insecure bool   `yaml:"insecure"`
}

// TelemetryMetricsConfig configures the metrics pipeline.
type TelemetryMetricsConfig struct {
	ExportIntervalSec int `yaml:"export_interval_sec"`
}

// TelemetryTracesConfig configures the trace pipeline.
type TelemetryTracesConfig struct {
	SamplingRatio float64 `yaml:"sampling_ratio"`
}
