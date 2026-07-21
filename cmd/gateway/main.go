package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dcotelessa/gateway/internal/config"
	"github.com/dcotelessa/gateway/internal/telemetry"
	"github.com/dcotelessa/gateway/internal/lsp"
	mcpserver "github.com/dcotelessa/gateway/internal/mcp"
	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/rest"
	"github.com/dcotelessa/gateway/internal/router"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Register -config flag before flag.Parse() so it's available to config.Load()
	flag.String("config", "", "Path to config.yaml (default: GATEWAY_CONFIG env → ./config.yaml → built-in defaults)")
	flag.Parse()

	// Load and validate configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway: config load error: %v\n", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: config validation error: %v\n", err)
		os.Exit(1)
	}

	// Initialise OpenTelemetry (no-op when disabled)
	telShutdown, err := telemetry.Init(context.Background(), telemetry.Config{
		Enabled: cfg.Telemetry.Enabled,
		Service: telemetry.ServiceConfig{
			Name:    cfg.Telemetry.Service.Name,
			Version: cfg.Telemetry.Service.Version,
		},
		OTLP: telemetry.OTLPConfig{
			Endpoint: cfg.Telemetry.OTLP.Endpoint,
			Insecure: cfg.Telemetry.OTLP.Insecure,
		},
		Metrics: telemetry.MetricsConfig{
			ExportIntervalSec: cfg.Telemetry.Metrics.ExportIntervalSec,
		},
		Traces: telemetry.TracesConfig{
			SamplingRatio: cfg.Telemetry.Traces.SamplingRatio,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway: telemetry init error: %v\n", err)
		os.Exit(1)
	}
	if err := telemetry.InitMetrics(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: telemetry metrics error: %v\n", err)
		os.Exit(1)
	}

	// Instantiate domain packages
	r := router.New()

	mm := modelmanager.New(modelmanager.ManagerConfig{
		ExecPath:         cfg.LlamaServer.ExecPath,
		HealthTimeoutSec: cfg.LlamaServer.HealthTimeoutSec,
		StopTimeoutSec:   cfg.LlamaServer.StopTimeoutSec,
		LogDir:           cfg.LlamaServer.LogDir,
		TotalVRAMMiB:     cfg.VRAM.TotalMiB,
		ReservedVRAMMiB:  cfg.VRAM.ReservedMiB,
		Models:           toMMModels(cfg.Models),
	})

	pol := policy.New(policy.PolicyConfig{
		RateLimitPerMin:     cfg.Policy.SessionRatePerMin,
		TokenLimitPerHour:   cfg.Policy.SessionTokensPerHour,
		SessionIdleTTLMin:   cfg.Policy.SessionIdleTTLMin,
		SweepIntervalMin:    cfg.Policy.BudgetSweepIntervalMin,
		BudgetDeepSeekDaily: cfg.Policy.BudgetDeepSeekDaily,
		BudgetGLMDaily:      cfg.Policy.BudgetGLMDaily,
	})

	// Register budget gauge — fires every metrics export interval
	if err := telemetry.RegisterBudgetGauge(func() map[string]int64 {
		return map[string]int64{
			"remote_deepseek": pol.TierRemaining("remote_deepseek"),
			"remote_glm":      pol.TierRemaining("remote_glm"),
		}
		// Local tiers omitted — slot-based, not token-based
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: budget gauge: %v\n", err)
	}

	lspCfg := lsp.DefaultConfig()
	lspCfg.IdleTimeoutMin = cfg.LSP.IdleTimeoutMin
	lspCfg.RequestTimeoutSec = cfg.LSP.RequestTimeoutSec
	lspCfg.InitTimeoutSec = cfg.LSP.InitTimeoutSec
	lspMgr := lsp.New(lspCfg)

	// Build MCP server
	mcpSrv := mcpserver.New(
		mcpserver.ServerConfig{
			EndpointPath:      cfg.MCP.EndpointPath,
			SessionIdleTTLMin: cfg.MCP.SessionIdleTTLMinutes,
		},
		r, mm, pol, lspMgr,
	)

	// Build root HTTP mux — MCP and REST share :8080
	mux := http.NewServeMux()

	// Mount MCP streamable HTTP handler
	mcpHTTP := server.NewStreamableHTTPServer(
		mcpSrv.MCPServer(),
		server.WithEndpointPath(cfg.MCP.EndpointPath),
		server.WithStateLess(false),
		server.WithSessionIdleTTL(
			time.Duration(cfg.MCP.SessionIdleTTLMinutes)*time.Minute,
		),
	)
	mux.Handle(cfg.MCP.EndpointPath, mcpHTTP)

	// Mount REST facade
	rest.RegisterRoutes(mux, rest.HandlerConfig{
		Router:   r,
		Manager:  mm,
		Policy:   pol,
		DrainSec: cfg.REST.ShutdownDrainSec,
	})

	// HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		fmt.Printf("gateway: listening on %s (MCP: %s, REST: /classify /implement /interpret)\n",
			addr, cfg.MCP.EndpointPath)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "gateway: server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigCh
	fmt.Printf("gateway: received %s — shutting down\n", sig)

	// Graceful shutdown sequence
	drainCtx, drainCancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.REST.ShutdownDrainSec)*time.Second,
	)
	defer drainCancel()

	if err := httpSrv.Shutdown(drainCtx); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: HTTP drain error: %v\n", err)
	}
	// Flush telemetry before model shutdown
	telCtx, telCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer telCancel()
	if err := telShutdown(telCtx); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: telemetry shutdown error: %v\n", err)
	}

	if err := mm.Shutdown(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: model manager shutdown error: %v\n", err)
	}
	lspMgr.Shutdown()
	pol.Stop()

	fmt.Println("gateway: shutdown complete")
}

func toMMModels(models map[string]config.ModelConfig) map[string]modelmanager.ModelConfig {
	out := make(map[string]modelmanager.ModelConfig, len(models))
	for tier, m := range models {
		out[tier] = modelmanager.ModelConfig{
			Path:               m.Path,
			VRAMRequirementMiB: m.VRAMRequirementMiB,
			ExtraArgs:          m.ExtraArgs,
			Port:               m.Port,
		}
	}
	return out
}
