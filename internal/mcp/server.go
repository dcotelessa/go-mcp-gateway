package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dcotelessa/gateway/internal/lsp"
	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/router"
)

// Server wraps the mcp-go MCP server with gateway domain dependencies.
type Server struct {
	mcp     *server.MCPServer
	handler *handlers
}

// ServerConfig holds configuration for the MCP server surface.
type ServerConfig struct {
	EndpointPath      string
	SessionIdleTTLMin int
}

// New creates an MCP server with all tools registered.
func New(
	cfg ServerConfig,
	r router.Router,
	mm *modelmanager.Manager,
	pol *policy.Registry,
	lspMgr *lsp.Manager,
) *Server {
	mcpServer := server.NewMCPServer(
		"local-model-gateway",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(
			"Gateway for local llama.cpp models and remote APIs. "+
				"Use route_complete to dispatch completions, "+
				"budget_status to check token budgets, "+
				"rate_status to check session rate limits.",
		),
	)

	h := &handlers{
		router:  r,
		manager: mm,
		policy:  pol,
		lspMgr:  lspMgr,
	}

	registerTools(mcpServer, h)

	return &Server{mcp: mcpServer, handler: h}
}

// MCPServer returns the underlying mcp-go server for HTTP mounting.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcp
}

// handlerFunc is the type our handlers implement.
type handlerFunc func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error)

// registerTools adds all gateway tools to the MCP server.
func registerTools(s *server.MCPServer, h *handlers) {
	s.AddTool(
		mcpgo.NewTool("route_complete",
			mcpgo.WithDescription("Route a completion to the appropriate model tier."),
			mcpgo.WithString("task", mcpgo.Required(),
				mcpgo.Description("Task description")),
			mcpgo.WithString("complexity", mcpgo.Required(),
				mcpgo.Description("Task complexity"),
				mcpgo.Enum("scaffold", "single_file", "multi_file", "recovery", "text_op")),
			mcpgo.WithString("force_tier",
				mcpgo.Description("Override tier selection")),
			mcpgo.WithString("session_id",
				mcpgo.Description("Session ID for rate limiting")),
		),
		safe(h.routeComplete),
	)

	s.AddTool(
		mcpgo.NewTool("budget_status",
			mcpgo.WithDescription("Query token budget status for model tiers."),
			mcpgo.WithString("tier",
				mcpgo.Description("Filter to a specific tier"),
				mcpgo.Enum("local_ornith", "local_qwen", "remote_deepseek", "remote_glm")),
		),
		safe(h.budgetStatus),
	)

	s.AddTool(
		mcpgo.NewTool("rate_status",
			mcpgo.WithDescription("Query rate limit status for a session."),
			mcpgo.WithString("session_id",
				mcpgo.Description("Session ID to query")),
		),
		safe(h.rateStatus),
	)

	s.AddTool(
		mcpgo.NewTool("lsp_open",
			mcpgo.WithDescription("Initialize or reuse an LSP session for a workspace."),
			mcpgo.WithString("workspace_root", mcpgo.Required(),
				mcpgo.Description("Absolute path to workspace root")),
			mcpgo.WithString("language", mcpgo.Required(),
				mcpgo.Description("Language: go|typescript"),
				mcpgo.Enum("go", "typescript")),
		),
		safe(h.lspOpen),
	)

	s.AddTool(
		mcpgo.NewTool("lsp_diagnostics",
			mcpgo.WithDescription("Get diagnostics for a file."),
			mcpgo.WithString("workspace_root", mcpgo.Required(),
				mcpgo.Description("Absolute path to workspace root")),
			mcpgo.WithString("language", mcpgo.Required(),
				mcpgo.Description("Language: go|typescript"),
				mcpgo.Enum("go", "typescript")),
			mcpgo.WithString("file_path", mcpgo.Required(),
				mcpgo.Description("Absolute path to file")),
		),
		safe(h.lspDiagnostics),
	)

	s.AddTool(
		mcpgo.NewTool("lsp_hover",
			mcpgo.WithDescription("Get hover information at a position."),
			mcpgo.WithString("workspace_root", mcpgo.Required(),
				mcpgo.Description("Absolute path to workspace root")),
			mcpgo.WithString("language", mcpgo.Required(),
				mcpgo.Description("Language: go|typescript"),
				mcpgo.Enum("go", "typescript")),
			mcpgo.WithString("file_path", mcpgo.Required(),
				mcpgo.Description("Absolute path to file")),
			mcpgo.WithNumber("line", mcpgo.Required(),
				mcpgo.Description("Zero-based line number")),
			mcpgo.WithNumber("character", mcpgo.Required(),
				mcpgo.Description("Zero-based character offset")),
		),
		safe(h.lspHover),
	)
}

// safe wraps a handler to ensure nil result + error becomes a structured error.
// Implements REQ-REG-003: no crash on nil result.
func safe(fn handlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := fn(ctx, req)
		if err != nil && result == nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return result, err
	}
}
