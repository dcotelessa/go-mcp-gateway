package filewriter

import (
	"context"
	"errors"
	"fmt"
)

// ErrParseExhausted means every rung of the retry ladder produced output
// that could not be parsed. It wraps the last *ParseError.
var ErrParseExhausted = errors.New("filewriter: all parse attempts exhausted")

// Run failure reasons, used by handlers to map a failure to a response.
const (
	// RunParseFailed means the ladder was exhausted on parse failures.
	RunParseFailed = "parse_failed"

	// RunWriteFailed means output parsed but could not be applied.
	RunWriteFailed = "write_failed"

	// RunGenerateFailed means a model call itself failed.
	RunGenerateFailed = "generate_failed"
)

// GenerateFn calls a model at the given tier. It is injected so this
// package does not depend on the model manager or the remote clients.
type GenerateFn func(ctx context.Context, tier, systemPrompt, userPrompt string) (string, error)

// PromptVariant selects the implement prompt template.
type PromptVariant string

const (
	// PromptStandard is used on the first attempt.
	PromptStandard PromptVariant = "standard"

	// PromptStrict is used on recovery attempts. It restates the output
	// contract more forcefully.
	PromptStrict PromptVariant = "strict"
)

// Prompts renders the implement prompt for one attempt.
type Prompts interface {
	Render(variant PromptVariant, tier string, in Input) (system, user string)
}

// Metrics receives execution events. Implemented in internal/telemetry.
// A nil Metrics is valid and records nothing.
type Metrics interface {
	FilesWritten(ctx context.Context, tier, complexity, operation string)
	WriteFailure(ctx context.Context, reason string)
	ParseFailure(ctx context.Context, reason, tier string)
	PhantomDelete(ctx context.Context, tier string)
	SpanFileWritten(ctx context.Context, path, operation string, bytes int)
}

// Input describes one implement task. The worktree is not part of it: the
// Writer is already bound to one, and a second copy here could disagree.
type Input struct {
	Task       string
	Complexity string
	Tier       string
	GraftCtx   string
}

// Result describes a successful run.
type Result struct {
	// Content is the accepted model response.
	Content string

	// Tier is the tier of the attempt whose output was accepted.
	Tier string

	// Attempts is the total number of model calls made.
	Attempts int

	// Changes is one entry per operation applied, in batch order.
	Changes []Change

	batch *Batch
}

// RunError is returned for every failed run.
type RunError struct {
	// Reason is RunParseFailed, RunWriteFailed, or RunGenerateFailed.
	Reason string

	// Attempts is the number of model calls made before failing.
	Attempts int

	// Tier is the tier of the last attempt.
	Tier string

	// Err is the cause. For parse failures it wraps both
	// ErrParseExhausted and the last *ParseError.
	Err error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("filewriter: run %s after %d attempt(s) at %s: %v",
		e.Reason, e.Attempts, e.Tier, e.Err)
}

func (e *RunError) Unwrap() error { return e.Err }

// ExecutorConfig wires an Executor.
type ExecutorConfig struct {
	Writer   *Writer    // required
	Prompts  Prompts    // required
	Generate GenerateFn // required

	// Parser defaults to NewParser().
	Parser *Parser

	// NextTier returns the escalation target for a tier. Nil disables
	// escalation, leaving a two-attempt ladder.
	NextTier func(tier string) (string, bool)

	// Metrics may be nil.
	Metrics Metrics
}

// Executor runs one implement task through the retry ladder: parse, write,
// and recover from unparseable output.
type Executor struct {
	parser   *Parser
	writer   *Writer
	prompts  Prompts
	generate GenerateFn
	nextTier func(string) (string, bool)
	metrics  Metrics
}

// NewExecutor validates cfg and returns an Executor.
func NewExecutor(cfg ExecutorConfig) (*Executor, error) {
	switch {
	case cfg.Writer == nil:
		return nil, errors.New("filewriter: executor requires a Writer")
	case cfg.Prompts == nil:
		return nil, errors.New("filewriter: executor requires Prompts")
	case cfg.Generate == nil:
		return nil, errors.New("filewriter: executor requires a GenerateFn")
	}

	e := &Executor{
		parser:   cfg.Parser,
		writer:   cfg.Writer,
		prompts:  cfg.Prompts,
		generate: cfg.Generate,
		nextTier: cfg.NextTier,
		metrics:  cfg.Metrics,
	}
	if e.parser == nil {
		e.parser = NewParser()
	}
	if e.nextTier == nil {
		e.nextTier = func(string) (string, bool) { return "", false }
	}
	if e.metrics == nil {
		e.metrics = noopMetrics{}
	}
	return e, nil
}

type rung struct {
	tier    string
	variant PromptVariant
}

// ladder returns the attempts for a task (RD-2): the original tier with the
// standard prompt, the original tier with the strict prompt, then the next
// tier up with the strict prompt when one exists.
func (e *Executor) ladder(tier string) []rung {
	rungs := []rung{
		{tier: tier, variant: PromptStandard},
		{tier: tier, variant: PromptStrict},
	}
	if next, ok := e.nextTier(tier); ok {
		rungs = append(rungs, rung{tier: next, variant: PromptStrict})
	}
	return rungs
}

// Run executes the task. It stops at the first attempt whose output parses.
//
// Parse failures move to the next rung. Write failures and model-call
// failures are terminal: retrying cannot fix an unsafe path or an
// unreachable model, and would only spend more.
func (e *Executor) Run(ctx context.Context, in Input) (*Result, error) {
	var (
		attempts  int
		lastTier  = in.Tier
		lastParse *ParseError
	)

	for _, r := range e.ladder(in.Tier) {
		if err := ctx.Err(); err != nil {
			return nil, &RunError{Reason: RunGenerateFailed, Attempts: attempts, Tier: lastTier, Err: err}
		}

		system, user := e.prompts.Render(r.variant, r.tier, in)
		attempts++
		lastTier = r.tier

		out, err := e.generate(ctx, r.tier, system, user)
		if err != nil {
			return nil, &RunError{Reason: RunGenerateFailed, Attempts: attempts, Tier: r.tier, Err: err}
		}

		ops, err := e.parser.Parse(out)
		if err != nil {
			var pe *ParseError
			if !errors.As(err, &pe) {
				return nil, &RunError{Reason: RunParseFailed, Attempts: attempts, Tier: r.tier, Err: err}
			}
			e.metrics.ParseFailure(ctx, pe.Reason, r.tier)
			lastParse = pe
			continue
		}

		batch, err := e.writer.Apply(ctx, ops)
		if err != nil {
			reason := ReasonIOError
			var we *WriteError
			if errors.As(err, &we) {
				reason = we.Reason
			}
			e.metrics.WriteFailure(ctx, reason)
			return nil, &RunError{Reason: RunWriteFailed, Attempts: attempts, Tier: r.tier, Err: err}
		}

		e.recordChanges(ctx, r.tier, in.Complexity, batch.Changes)
		return &Result{
			Content:  out,
			Tier:     r.tier,
			Attempts: attempts,
			Changes:  batch.Changes,
			batch:    batch,
		}, nil
	}

	return nil, &RunError{
		Reason:   RunParseFailed,
		Attempts: attempts,
		Tier:     lastTier,
		Err:      fmt.Errorf("%w: %w", ErrParseExhausted, lastParse),
	}
}

func (e *Executor) recordChanges(ctx context.Context, tier, complexity string, changes []Change) {
	for _, c := range changes {
		if c.Skipped {
			e.metrics.PhantomDelete(ctx, tier)
			continue
		}
		e.metrics.FilesWritten(ctx, tier, complexity, string(c.Operation))
		e.metrics.SpanFileWritten(ctx, c.Path, string(c.Operation), c.Bytes)
	}
}

type noopMetrics struct{}

func (noopMetrics) FilesWritten(context.Context, string, string, string) {}
func (noopMetrics) WriteFailure(context.Context, string)                 {}
func (noopMetrics) ParseFailure(context.Context, string, string)         {}
func (noopMetrics) PhantomDelete(context.Context, string)                {}
func (noopMetrics) SpanFileWritten(context.Context, string, string, int) {}
