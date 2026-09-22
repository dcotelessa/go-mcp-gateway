package filewriter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// --- test doubles ---------------------------------------------------------

type call struct {
	tier    string
	variant PromptVariant
}

// fakePrompts embeds the variant in the system prompt so fakeModel can
// record which variant each call used.
type fakePrompts struct{}

func (fakePrompts) Render(v PromptVariant, tier string, in Input) (string, string) {
	return string(v), in.Task
}

// fakeModel returns scripted responses in order and records every call.
type fakeModel struct {
	mu        sync.Mutex
	responses []string
	err       error
	calls     []call
}

func (m *fakeModel) generate(_ context.Context, tier, system, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, call{tier: tier, variant: PromptVariant(system)})
	if m.err != nil {
		return "", m.err
	}
	i := len(m.calls) - 1
	if i >= len(m.responses) {
		return "no blocks here", nil
	}
	return m.responses[i], nil
}

type recordedMetrics struct {
	mu            sync.Mutex
	parseFailures []call // variant field unused; tier recorded
	parseReasons  []string
	writeFailures []string
	filesWritten  []string
	phantom       int
	spanEvents    int
}

func (r *recordedMetrics) FilesWritten(_ context.Context, _, _, op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filesWritten = append(r.filesWritten, op)
}
func (r *recordedMetrics) WriteFailure(_ context.Context, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeFailures = append(r.writeFailures, reason)
}
func (r *recordedMetrics) ParseFailure(_ context.Context, reason, tier string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parseFailures = append(r.parseFailures, call{tier: tier})
	r.parseReasons = append(r.parseReasons, reason)
}
func (r *recordedMetrics) PhantomDelete(context.Context, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phantom++
}
func (r *recordedMetrics) SpanFileWritten(context.Context, string, string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spanEvents++
}

const (
	unparseable = "I would restructure the swap queue like this, but I will describe it in prose."
	goodOutput  = "```file:internal/a.go\npackage a\n```\n"
)

func escalateTo(next string) func(string) (string, bool) {
	return func(tier string) (string, bool) {
		if tier == "small" {
			return next, true
		}
		return "", false
	}
}

func newTestExecutor(t *testing.T, root string, m *fakeModel, next func(string) (string, bool), met Metrics) *Executor {
	t.Helper()
	e, err := NewExecutor(ExecutorConfig{
		Writer:   newWriter(t, root),
		Prompts:  fakePrompts{},
		Generate: m.generate,
		NextTier: next,
		Metrics:  met,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return e
}

func runErr(t *testing.T, err error) *RunError {
	t.Helper()
	var re *RunError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RunError, got %T: %v", err, err)
	}
	return re
}

// --- scenarios ------------------------------------------------------------

// RETRY-5 (4.2) — Exhaustion wraps both the sentinel and the last ParseError.
func TestErrParseExhausted_WrapsLastParseError(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, unparseable, unparseable}}

	res, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})

	if res != nil {
		t.Fatal("expected nil result on exhaustion")
	}
	if !errors.Is(err, ErrParseExhausted) {
		t.Errorf("errors.Is(err, ErrParseExhausted) = false; err = %v", err)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Reason != ReasonNoOperations {
		t.Errorf("expected wrapped *ParseError with no_operations, got %v", err)
	}
	if re := runErr(t, err); re.Reason != RunParseFailed || re.Attempts != 3 {
		t.Errorf("RunError = %+v", re)
	}
	if len(snapshotTree(t, root)) != 0 {
		t.Error("files were written despite exhaustion")
	}
}

// RETRY-1 — First failure retries the same tier with the strict prompt.
func TestRun_RetriesSameTierWithStrictPrompt(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, goodOutput}}

	if _, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []call{{"small", PromptStandard}, {"small", PromptStrict}}
	if len(m.calls) != len(want) {
		t.Fatalf("calls = %+v, want %+v", m.calls, want)
	}
	for i := range want {
		if m.calls[i] != want[i] {
			t.Errorf("call[%d] = %+v, want %+v", i, m.calls[i], want[i])
		}
	}
}

// RETRY-2 — Same-tier retry success applies files.
func TestRun_SameTierRetryAppliesFiles(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, goodOutput}}

	res, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Tier != "small" || res.Attempts != 2 {
		t.Errorf("tier=%q attempts=%d, want small/2", res.Tier, res.Attempts)
	}
	if got := readFile(t, filepath.Join(root, "internal/a.go")); got != "package a" {
		t.Errorf("content = %q", got)
	}
}

// RETRY-3 — Second failure escalates one tier with the strict prompt.
func TestRun_EscalatesAfterSecondFailure(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, unparseable, goodOutput}}

	if _, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(m.calls))
	}
	if m.calls[2] != (call{"large", PromptStrict}) {
		t.Errorf("third call = %+v, want large/strict", m.calls[2])
	}
}

// RETRY-4 — Escalated success records the escalated tier.
func TestRun_EscalatedSuccessRecordsTier(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, unparseable, goodOutput}}
	met := &recordedMetrics{}

	res, err := newTestExecutor(t, root, m, escalateTo("large"), met).
		Run(context.Background(), Input{Task: "t", Tier: "small"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Tier != "large" || res.Attempts != 3 {
		t.Errorf("tier=%q attempts=%d, want large/3", res.Tier, res.Attempts)
	}
	if len(met.parseFailures) != 2 {
		t.Fatalf("parse failures recorded = %d, want 2", len(met.parseFailures))
	}
	for _, pf := range met.parseFailures {
		if pf.tier != "small" {
			t.Errorf("parse failure tier = %q, want small", pf.tier)
		}
	}
}

// RETRY-5 — Exhaustion writes nothing and returns empty changes.
func TestRun_AllAttemptsFailExhaustion(t *testing.T) {
	root := newWorktree(t, map[string]string{"keep.go": "package keep\n"})
	before := snapshotTree(t, root)
	m := &fakeModel{responses: []string{unparseable, unparseable, unparseable}}

	res, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})
	if res != nil || !errors.Is(err, ErrParseExhausted) {
		t.Fatalf("res=%v err=%v; want nil and ErrParseExhausted", res, err)
	}
	assertTreeEqual(t, before, snapshotTree(t, root))
}

// RETRY-6 — A clean first parse makes a single call.
func TestRun_CleanParseSingleCall(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{goodOutput}}

	res, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.calls) != 1 || m.calls[0].variant != PromptStandard {
		t.Errorf("calls = %+v, want one standard call", m.calls)
	}
	if res.Attempts != 1 {
		t.Errorf("attempts = %d", res.Attempts)
	}
}

// RETRY-7 — The top tier has no escalation: two attempts only.
func TestRun_TopTierTwoAttemptsOnly(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, unparseable, goodOutput}}

	_, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "large"})
	if !errors.Is(err, ErrParseExhausted) {
		t.Fatalf("expected exhaustion, got %v", err)
	}
	if len(m.calls) != 2 {
		t.Errorf("calls = %d, want 2", len(m.calls))
	}
}

// RETRY-8 — Write failures are terminal and never re-prompt.
func TestRun_WriteFailureTerminalNoReprompt(t *testing.T) {
	root := newWorktree(t, nil)
	escaping := "```file:../escaped.go\npackage escaped\n```\n"
	m := &fakeModel{responses: []string{escaping, goodOutput, goodOutput}}
	met := &recordedMetrics{}

	_, err := newTestExecutor(t, root, m, escalateTo("large"), met).
		Run(context.Background(), Input{Task: "t", Tier: "small"})

	re := runErr(t, err)
	if re.Reason != RunWriteFailed {
		t.Errorf("reason = %q, want %q", re.Reason, RunWriteFailed)
	}
	if len(m.calls) != 1 {
		t.Errorf("calls = %d, want 1 — write failures must not re-prompt", len(m.calls))
	}
	if len(met.writeFailures) != 1 || met.writeFailures[0] != ReasonPathTraversal {
		t.Errorf("write failures = %v", met.writeFailures)
	}
	if _, statErr := os.Lstat(filepath.Join(filepath.Dir(root), "escaped.go")); statErr == nil {
		t.Error("escaped file was written")
	}
}

// --- beyond the spec --------------------------------------------------------

// A failed model call is terminal: retrying an unreachable model only spends.
func TestRun_GenerateErrorIsTerminal(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{err: errors.New("upstream 502")}

	_, err := newTestExecutor(t, root, m, escalateTo("large"), nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})

	if re := runErr(t, err); re.Reason != RunGenerateFailed || re.Attempts != 1 {
		t.Errorf("RunError = %+v", re)
	}
	if len(m.calls) != 1 {
		t.Errorf("calls = %d, want 1", len(m.calls))
	}
}

func TestRun_CancelledContextMakesNoCalls(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{goodOutput}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestExecutor(t, root, m, nil, nil).Run(ctx, Input{Task: "t", Tier: "small"})
	if err == nil || len(m.calls) != 0 {
		t.Errorf("err=%v calls=%d; want error and no calls", err, len(m.calls))
	}
}

// Nil NextTier means no escalation.
func TestRun_NilNextTierMeansTwoAttempts(t *testing.T) {
	root := newWorktree(t, nil)
	m := &fakeModel{responses: []string{unparseable, unparseable, goodOutput}}

	_, err := newTestExecutor(t, root, m, nil, nil).
		Run(context.Background(), Input{Task: "t", Tier: "small"})
	if !errors.Is(err, ErrParseExhausted) || len(m.calls) != 2 {
		t.Errorf("err=%v calls=%d; want exhaustion after 2", err, len(m.calls))
	}
}

// Skipped deletes are phantoms, not writes.
func TestRun_SkippedDeleteRecordedAsPhantom(t *testing.T) {
	root := newWorktree(t, nil)
	out := goodOutput + "```file-delete:never_existed.go\n```\n"
	m := &fakeModel{responses: []string{out}}
	met := &recordedMetrics{}

	res, err := newTestExecutor(t, root, m, nil, met).
		Run(context.Background(), Input{Task: "t", Tier: "small", Complexity: "single_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if met.phantom != 1 {
		t.Errorf("phantom deletes = %d, want 1", met.phantom)
	}
	if len(met.filesWritten) != 1 || met.spanEvents != 1 {
		t.Errorf("filesWritten=%v spanEvents=%d, want 1 each", met.filesWritten, met.spanEvents)
	}
	if len(res.Changes) != 2 {
		t.Errorf("changes = %d, want 2", len(res.Changes))
	}
}

func TestNewExecutor_RequiresDependencies(t *testing.T) {
	w := newWriter(t, newWorktree(t, nil))
	gen := (&fakeModel{}).generate

	cases := map[string]ExecutorConfig{
		"no writer":   {Prompts: fakePrompts{}, Generate: gen},
		"no prompts":  {Writer: w, Generate: gen},
		"no generate": {Writer: w, Prompts: fakePrompts{}},
	}
	for name, cfg := range cases {
		if _, err := NewExecutor(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
