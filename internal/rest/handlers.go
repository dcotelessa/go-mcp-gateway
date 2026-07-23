package rest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dcotelessa/gateway/internal/modelmanager"
	"github.com/dcotelessa/gateway/internal/remote"
	"github.com/dcotelessa/gateway/internal/policy"
	"github.com/dcotelessa/gateway/internal/router"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// HandlerConfig holds dependencies for the REST handlers.
type HandlerConfig struct {
	Router   router.Router
	Manager  *modelmanager.Manager
	Policy   *policy.Registry
	Resolver *remote.Resolver
	DrainSec int
}

// RegisterRoutes mounts all REST routes on mux.
// Shares the same mux as the MCP server — no /mcp collision by design.
func RegisterRoutes(mux *http.ServeMux, cfg HandlerConfig) {
	h := &restHandlers{cfg: cfg}

	mux.HandleFunc("/classify", h.methodGate("POST", h.classify))
	mux.HandleFunc("/implement", h.methodGate("POST", h.implement))
	mux.HandleFunc("/interpret", h.methodGate("POST", h.interpret))
	mux.HandleFunc("/", h.notFound)
}

type restHandlers struct {
	cfg HandlerConfig
}

// methodGate returns a handler that enforces a single HTTP method.
func (h *restHandlers) methodGate(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed,
				"method_not_allowed",
				fmt.Sprintf("only %s is accepted", method))
			return
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType,
				"unsupported_media_type",
				"Content-Type must be application/json")
			return
		}
		next(w, r)
	}
}

// sessionKey derives a stable session key from REST request headers.
// Priority: X-Session-Id → Authorization hash → "rest-anonymous" (REQ-REST-009).
func sessionKey(r *http.Request) string {
	if sid := r.Header.Get("X-Session-Id"); sid != "" {
		return sid
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		sum := sha256.Sum256([]byte(auth))
		return fmt.Sprintf("rest-auth-%x", sum[:8])
	}
	return "rest-anonymous"
}

// --- /classify ---

func (h *restHandlers) classify(w http.ResponseWriter, r *http.Request) {
	tracer := otel.Tracer("github.com/dcotelessa/gateway")
	ctx, span := tracer.Start(r.Context(), "gateway.rest.classify")
	defer span.End()
	span.SetAttributes(semconv.HTTPRequestMethodKey.String(r.Method))

	var req ClassifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Task == "" {
		span.SetStatus(codes.Error, "missing task")
		writeError(w, http.StatusBadRequest, "missing_task", "task field is required")
		return
	}

	_ = ctx // propagated via span; Router.Classify doesn't take ctx yet
	result, err := h.cfg.Router.Classify(req.Task)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		writeError(w, http.StatusInternalServerError, "classify_error", err.Error())
		return
	}

	span.SetAttributes(
		attribute.String("gateway.complexity", string(result.Complexity)),
		attribute.String("gateway.qa_level", string(result.QALevel)),
	)
	span.SetStatus(codes.Ok, "")

	writeJSON(w, http.StatusOK, ClassifyResponse{
		Complexity: string(result.Complexity),
		QALevel:    string(result.QALevel),
	})
}

// --- /implement ---

func (h *restHandlers) implement(w http.ResponseWriter, r *http.Request) {
	var req ImplementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	sid := sessionKey(r)

	// Check session limits
	if err := h.cfg.Policy.CheckSession(sid); err != nil {
		tags := reasoningTags(err)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("%s (tags: %v)", err.Error(), tags))
		return
	}

	// Classify the task to determine tier
	classifyResult, err := h.cfg.Router.Classify(req.Task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "classify_error", err.Error())
		return
	}

	routeResult, err := h.cfg.Router.Route(classifyResult.Complexity, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "route_error", err.Error())
		return
	}

	// Ensure local model loaded if needed
	var content string
	var totalTokens int

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	switch routeResult.Tier {
	case router.TierLocalOrnith, router.TierLocalQwen:
		_, loadErr := h.cfg.Manager.EnsureLoaded(ctx, string(routeResult.Tier))
		if loadErr != nil {
			writeError(w, http.StatusServiceUnavailable, "model_not_loaded", loadErr.Error())
			return
		}
		resp, compErr := h.cfg.Manager.Complete(ctx, modelmanager.CompletionRequest{
			Messages: []modelmanager.ChatMessage{
				{Role: "user", Content: req.Task},
			},
		})
		if compErr != nil {
			writeError(w, http.StatusInternalServerError, "completion_error", compErr.Error())
			return
		}
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}
		totalTokens = resp.Usage.TotalTokens
	default:
		// Remote tier — dispatch via resolver
		if h.cfg.Resolver == nil {
			writeError(w, http.StatusServiceUnavailable, "no_resolver",
				"remote resolver not configured")
			return
		}
		adapter, resolveErr := h.cfg.Resolver.Resolve(string(routeResult.Tier))
		if resolveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "tier_unavailable", resolveErr.Error())
			return
		}
		remoteResult, remoteErr := adapter.Do(remote.RemoteRequest{
			Task:       req.Task,
			Tier:       string(routeResult.Tier),
			Complexity: string(classifyResult.Complexity),
		})
		if remoteErr != nil {
			writeError(w, http.StatusBadGateway, "remote_error", remoteErr.Error())
			return
		}
		content = remoteResult.Content
		totalTokens = remoteResult.TotalTokens()
	}

	h.cfg.Policy.DeductSession(sid, totalTokens)
	h.cfg.Policy.DeductTier(string(routeResult.Tier), totalTokens)

	tags := append(req.ReasoningTags, routeResult.ReasoningTags...)

	writeJSON(w, http.StatusOK, ImplementResponse{
		FilesChanged:  req.Files,
		Status:        "complete",
		ReasoningTags: tags,
		Content:       content,
		TotalTokens:   totalTokens,
	})
}

// --- /interpret ---

func (h *restHandlers) interpret(w http.ResponseWriter, r *http.Request) {
	var req InterpretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	verdict, err := h.cfg.Router.Interpret(req.VitestOutput, req.Diff, req.Scenarios)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "interpret_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, InterpretResponse{
		TaskID:     verdict.TaskID,
		Status:     verdict.Status,
		Failures:   verdict.Failures,
		Hint:       verdict.Hint,
		NextAction: verdict.NextAction,
	})
}

// --- 404 ---

func (h *restHandlers) notFound(w http.ResponseWriter, r *http.Request) {
	// Only fire for paths not matched by other handlers
	if r.URL.Path == "/" || r.URL.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "no route matched")
		return
	}
	writeError(w, http.StatusNotFound, "not_found",
		fmt.Sprintf("no route for %s", r.URL.Path))
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, ErrorResponse{Error: code, Detail: detail})
}

func reasoningTags(err error) []string {
	type tagger interface {
		ReasoningTags() []string
	}
	if t, ok := err.(tagger); ok {
		return t.ReasoningTags()
	}
	return nil
}
