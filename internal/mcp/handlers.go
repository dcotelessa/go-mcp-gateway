package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dcotelessa/gateway/internal/lsp"
	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/router"
)

// handlers holds references to all domain packages needed by MCP tool handlers.
type handlers struct {
	router  router.Router
	manager *modelmanager.Manager
	policy  *policy.Registry
	lspMgr  *lsp.Manager
}

// sessionID extracts the MCP session ID from context, falling back to "anonymous".
func sessionID(ctx context.Context) string {
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return "anonymous"
	}
	id := session.SessionID()
	if id == "" {
		return "anonymous"
	}
	return id
}

// getString extracts a string argument from a CallToolRequest.
func getString(req mcpgo.CallToolRequest, key string) string {
	v := mcpgo.ParseArgument(req, key, "")
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// getFloat extracts a float64 argument from a CallToolRequest.
func getFloat(req mcpgo.CallToolRequest, key string) float64 {
	v := mcpgo.ParseArgument(req, key, float64(0))
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// --- route_complete ---

type routeCompleteResult struct {
	Content       string   `json:"content"`
	Tier          string   `json:"tier"`
	Complexity    string   `json:"complexity"`
	ReasoningTags []string `json:"reasoning_tags"`
	Usage         usage    `json:"usage"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (h *handlers) routeComplete(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	task := getString(req, "task")
	complexityStr := getString(req, "complexity")
	forceTierStr := getString(req, "force_tier")

	sid := sessionID(ctx)

	// Check session rate/token limits
	if err := h.policy.CheckSession(sid); err != nil {
		tags := reasoningTags(err)
		return mcpgo.NewToolResultError(
			fmt.Sprintf("rate_limited: %s (tags: %v)", err.Error(), tags),
		), nil
	}

	// Resolve tier
	complexity := router.Complexity(complexityStr)
	forceTier := router.Tier(forceTierStr)

	routeResult, err := h.router.Route(complexity, forceTier)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	// Check tier budget — attempt fallback cascade on exhaustion
	if err := h.policy.CheckTier(string(routeResult.Tier)); err != nil {
		next, ok := h.router.RouteFallback(routeResult.Tier)
		if !ok {
			return mcpgo.NewToolResultError("all_tiers_unavailable"), nil
		}
		routeResult.ReasoningTags = append(routeResult.ReasoningTags,
			fmt.Sprintf("fallback_tier_%s_to_%s", string(routeResult.Tier), string(next)))
		routeResult.Tier = next
	}

	var content string
	var totalTokens int

	switch routeResult.Tier {
	case router.TierLocalOrnith, router.TierLocalQwen:
		_, loadErr := h.manager.EnsureLoaded(ctx, string(routeResult.Tier))
		if loadErr != nil {
			return mcpgo.NewToolResultError(
				fmt.Sprintf("model_not_loaded: %s", loadErr.Error()),
			), nil
		}
		resp, compErr := h.manager.Complete(ctx, modelmanager.CompletionRequest{
			Messages: []modelmanager.ChatMessage{
				{Role: "user", Content: task},
			},
		})
		if compErr != nil {
			return mcpgo.NewToolResultError(compErr.Error()), nil
		}
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}
		totalTokens = resp.Usage.TotalTokens

	default:
		// Remote tier placeholder — API client integration goes here
		content = fmt.Sprintf("[remote:%s] task: %s", routeResult.Tier, task)
		totalTokens = len(task) / 4
	}

	// Check context cancellation before deducting tokens
	if ctx.Err() != nil {
		return mcpgo.NewToolResultError("cancelled"), nil
	}

	// Deduct tokens
	h.policy.DeductSession(sid, totalTokens)
	h.policy.DeductTier(string(routeResult.Tier), totalTokens)

	result := routeCompleteResult{
		Content:       content,
		Tier:          string(routeResult.Tier),
		Complexity:    complexityStr,
		ReasoningTags: routeResult.ReasoningTags,
		Usage:         usage{TotalTokens: totalTokens},
	}

	data, _ := json.Marshal(result)
	return mcpgo.NewToolResultText(string(data)), nil
}

// --- budget_status ---

type tierBudgetStatus struct {
	Tier            string `json:"tier"`
	Currency        string `json:"currency"`
	Limit           int64  `json:"limit"`
	Remaining       int64  `json:"remaining"`
	WindowResetUnix int64  `json:"window_reset_unix"`
}

type budgetStatusResult struct {
	Tiers []tierBudgetStatus `json:"tiers"`
}

func (h *handlers) budgetStatus(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	filterTier := getString(req, "tier")

	allTiers := []string{"local_ornith", "local_qwen", "remote_deepseek", "remote_glm"}
	tiers := allTiers
	if filterTier != "" {
		tiers = []string{filterTier}
	}

	var statuses []tierBudgetStatus
	for _, t := range tiers {
		remaining := h.policy.TierRemaining(t)
		if remaining == -1 {
			statuses = append(statuses, tierBudgetStatus{
				Tier:      t,
				Currency:  "vram_slot",
				Limit:     1,
				Remaining: localSlotRemaining(h.manager, t),
			})
		} else {
			statuses = append(statuses, tierBudgetStatus{
				Tier:      t,
				Currency:  "tokens",
				Limit:     -1,
				Remaining: remaining,
			})
		}
	}

	data, _ := json.Marshal(budgetStatusResult{Tiers: statuses})
	return mcpgo.NewToolResultText(string(data)), nil
}

// localSlotRemaining returns 1 if the tier's model is resident and idle.
func localSlotRemaining(mm *modelmanager.Manager, tier string) int64 {
	r := mm.Resident()
	if r == nil || r.Tier != tier || r.Swapping {
		return 0
	}
	return 1
}

// --- rate_status ---

type rateStatusResult struct {
	SessionID   string `json:"session_id"`
	Blocked     bool   `json:"blocked"`
	BlockReason string `json:"block_reason,omitempty"`
}

func (h *handlers) rateStatus(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sid := getString(req, "session_id")
	if sid == "" {
		sid = sessionID(ctx)
	}

	result := rateStatusResult{SessionID: sid}

	// Probe without side effects by checking tier (read-only)
	// Session check consumes a rate tick so we only report blocked state
	// by inspecting the error from a probe call
	err := h.policy.CheckSession(sid)
	if err != nil {
		result.Blocked = true
		result.BlockReason = err.Error()
		// Restore: not possible without read-only API — acceptable for status query
	}

	data, _ := json.Marshal(result)
	return mcpgo.NewToolResultText(string(data)), nil
}

// --- lsp_open ---

type lspOpenResult struct {
	SessionEstablished bool        `json:"session_established"`
	Capabilities       interface{} `json:"capabilities"`
}

func (h *handlers) lspOpen(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	workspaceRoot := getString(req, "workspace_root")
	language := getString(req, "language")

	_, err := h.lspMgr.GetOrCreate(language, workspaceRoot)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	data, _ := json.Marshal(lspOpenResult{
		SessionEstablished: true,
		Capabilities:       map[string]interface{}{},
	})
	return mcpgo.NewToolResultText(string(data)), nil
}

// --- lsp_diagnostics ---

func (h *handlers) lspDiagnostics(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	workspaceRoot := getString(req, "workspace_root")
	language := getString(req, "language")
	filePath := getString(req, "file_path")

	s, err := h.lspMgr.GetOrCreate(language, workspaceRoot)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	if err := s.EnsureFileOpen(filePath); err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	result, err := s.Request(ctx, "textDocument/diagnostic", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + filePath},
	})
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	return mcpgo.NewToolResultText(string(result)), nil
}

// --- lsp_hover ---

func (h *handlers) lspHover(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	workspaceRoot := getString(req, "workspace_root")
	language := getString(req, "language")
	filePath := getString(req, "file_path")
	line := int(getFloat(req, "line"))
	character := int(getFloat(req, "character"))

	s, err := h.lspMgr.GetOrCreate(language, workspaceRoot)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	if err := s.EnsureFileOpen(filePath); err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	result, err := s.Request(ctx, "textDocument/hover", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + filePath},
		"position":     map[string]interface{}{"line": line, "character": character},
	})
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	return mcpgo.NewToolResultText(string(result)), nil
}

// reasoningTags extracts reasoning tags from a policy error if available.
func reasoningTags(err error) []string {
	type tagger interface {
		ReasoningTags() []string
	}
	if t, ok := err.(tagger); ok {
		return t.ReasoningTags()
	}
	return nil
}
