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
	"github.com/dcotelessa/gateway/internal/lsp"
	mcpserver "github.com/dcotelessa/gateway/internal/mcp"
	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/rest"
	"github.com/dcotelessa/gateway/internal/router"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
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

	// 1. Stop accepting new HTTP connections, drain in-flight
	if err := httpSrv.Shutdown(drainCtx); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: HTTP drain error: %v\n", err)
	}

	// 2. Shutdown model manager (SIGTERM resident process)
	if err := mm.Shutdown(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: model manager shutdown error: %v\n", err)
	}

	// 3. Shutdown LSP sessions
	lspMgr.Shutdown()

	// 4. Stop policy sweep goroutine
	pol.Stop()

	fmt.Println("gateway: shutdown complete")
}

// toMMModels converts config.ModelConfig map to modelmanager.ModelConfig map.
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
