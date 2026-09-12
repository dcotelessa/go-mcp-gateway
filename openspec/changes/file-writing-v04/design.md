# Design: v0.4 File Writing — Closing the Implementation Loop

## Research decisions implemented (binding)

| # | Decision | How this design applies it |
|---|----------|----------------------------|
| RD-1 | Full file replacement. Not unified diff patches, not search/replace blocks. | The output contract is whole-file bodies in `file:` fences. The parser never merges fragments; the writer never patches. Op kinds are `create` / `modify` / `delete` only, where `modify` means "replace entire file". |
| RD-2 | Parse failure: retry once on the same tier with a stricter prompt, then escalate to the next tier up. | The executor implements a fixed ladder: `(tier, standard)` → `(tier, strict)` → `(nextTier, strict)`, max 3 model calls, stop on first parse success. Recovery is driven by the structured `*ParseError`, never string sniffing. Write failures are terminal and never re-prompted. |

### Open questions resolved for v0.4 (binding, do not revisit)

| Open question | Resolution |
|---|---|
| Q3: staged temp dir vs direct write + rollback | Stage each new content to a sibling temp file (`.gw-tmp-*`) in the target directory (same filesystem), then rename-swap into place. Originals are snapshotted in memory before swapping for rollback. No staging directory pollutes the worktree. |
| Q4: does the gateway run `git add`? | No. The gateway performs plain filesystem writes only. Staging and commit remain the Mastra workflow's job. The diff is generated in-process so the git index is never touched. |
| Q5: dry-run mode | Yes, as a library API (`Writer.DryRun`) used by tests and future prompt-reliability tooling. Not exposed on the REST/MCP contract in v0.4. |
| Diff implementation | In-process unified diff via `github.com/pmezard/go-difflib v1.0.0`. No `git diff` subprocess, no index side effects, deterministic headers. |

## Technical approach

```
POST /implement  (or MCP route_complete)
  → policy/budget check            (unchanged, internal/policy)
  → tier selection                 (unchanged, internal/router)
  → graft enrichment               (unchanged, internal/graft)
  → filewriter.Executor.Run:
       ladder = [(T, standard), (T, strict), (next(T), strict)]
       for each (tier, prompt) in ladder:
           out   = Generate(tier, prompt)          // injected; calls modelmanager path
           ops   = Parser.Parse(out)               // *ParseError → parse_failures, next rung
           batch = Writer.Apply(ctx, ops)          // validate ALL paths first
                                             // any WriteError → write_failures, TERMINAL
           diff  = batch.Diff()
           → files_written per change; span event per file
       all rungs fail → ErrParseExhausted → handler maps to reason "parse_failed"
  → response: {…existing fields…, files_changed, diff}   (additive)
  → remote /interpret payload gains diff            (internal/remote)
```

## Package structure (no new top-level packages beyond internal/filewriter)

```
internal/filewriter/
  types.go      OpKind consts (create|modify|delete), FileOp, Change,
                ParseError (+ reason consts), WriteError (+ reason consts),
                ErrParseExhausted, Metrics interface
  parser.go     Parser.Parse(output string) ([]FileOp, error)   // fence scanner
  pathsafe.go   PathValidator, SafetyConfig, Resolve(raw) (rel, abs, err)
  writer.go     Writer: Plan / DryRun (validation-only path)
  apply.go      Writer.Apply: snapshot → stage temps → swap → rollback; Batch
  diff.go       Batch.Diff(context int) string                  // go-difflib
  executor.go   Executor.Run: attempt ladder, Metrics hooks, Result
  prompt.go     PromptRenderer: standard + strict templates (embedded consts)
  *_test.go     one test function per spec scenario (PARSE-*, SAFE-*, …)

internal/router/nexttier.go   NextTierUp(tier) (string, bool)   // ADDITIVE read-only lookup
internal/telemetry/           +3 counters, +FilewriterMetrics adapter (implements filewriter.Metrics)
internal/config/              +FileWriterConfig{BinaryExtensions, DiffContextLines, PromptOverrides}
internal/rest/                /implement handler: build executor, map Result → response
internal/mcp/                 route_complete handler: same executor, tool result incl. files_changed/diff
internal/remote/              /interpret payload gains diff field
cmd/gateway/                  register defaults + instruments
```

## Key types

```go
package filewriter

type OpKind string // "create" | "modify" | "delete"

type FileOp struct {
    Kind    OpKind
    RawPath string // exactly as emitted
    Path    string // worktree-relative, cleaned
    Content string // complete new file; empty for delete
}

type ParseError struct {
    Reason string // no_operations|unclosed_fence|missing_path|empty_content|duplicate_path
    Detail string
    Line   int    // best-effort 1-based line of the offending fence
}

type WriteError struct {
    Reason string // path_traversal|gitignored|binary_extension|not_regular_file|delete_missing|io_error
    Path   string
    Err    error
}

type Change struct {
    Path      string
    Operation OpKind
    Bytes     int
}

type Batch struct {
    Changes []Change
    // unexported: before/after snapshots + order, captured by Apply
}
func (b *Batch) Diff(contextLines int) string

type GenerateFn func(ctx context.Context, tier, systemPrompt, userPrompt string) (string, error)

type Metrics interface { // implemented by adapter in internal/telemetry; nil-safe
    FilesWritten(ctx context.Context, tier, complexity, operation string)
    WriteFailure(ctx context.Context, reason string)
    ParseFailure(ctx context.Context, reason, tier string)
    SpanFileWritten(ctx context.Context, path, operation string, bytes int)
}

type Input struct {
    Task       string
    Complexity string
    Tier       string
    Worktree   string
    GraftCtx   string
}

type Executor struct {
    Parser   *Parser
    Writer   *Writer
    Prompts  *PromptRenderer
    Generate GenerateFn
    NextTier func(tier string) (string, bool)
    Metrics  Metrics
}
var ErrParseExhausted = errors.New("filewriter: all parse attempts exhausted")
func (e *Executor) Run(ctx context.Context, in Input) (*Result, error)

type Result struct {
    Content  string   // accepted model response (returned as `content`, unchanged)
    Tier     string   // tier of the ACCEPTED attempt
    Attempts int
    Changes  []Change
    Diff     string
}
```

```go
// internal/router/nexttier.go — additive, derives from the existing cascade
// ordering constant. Routing, fallback cascade, and tier selection are untouched.
func NextTierUp(tier string) (string, bool)
```

## Output contract (normative grammar)

```
response := ( prose | fence )*
fence    := "```" info "\n" body "\n" "```"
info     := "file:" path | "file-delete:" path
body     := complete new file content        (empty body allowed only for file-delete)
```

- Only `file:` / `file-delete:` fences are operations; every other fence is ignored.
- Kind classification at parse time: target exists (regular file) → `modify`; absent → `create` (existence checked via the validator's resolved path, inside the worktree).
- Two ops resolving to the same relative path → `duplicate_path` parse failure.

## Error taxonomy → metric mapping

| Layer | Reasons | Metric |
|---|---|---|
| Parse | `no_operations`, `unclosed_fence`, `missing_path`, `empty_content`, `duplicate_path` | `gateway.task.parse_failures{reason, tier}`; drives RD-2 ladder |
| Write | `path_traversal`, `gitignored`, `binary_extension`, `not_regular_file`, `delete_missing`, `io_error` | `gateway.task.write_failures{reason}`; terminal, never re-prompted |
| Handler | `parse_failed`, `write_failed` (+ `attempts`) | response failure mapping |

## Architectural decisions

1. **filewriter owns contract + recovery; handlers stay thin.** REST and MCP must behave identically, so all parsing, path safety, atomicity, diffing, and the RD-2 ladder live in `internal/filewriter`. Handlers only build the executor and map results.
2. **Model calls are injected.** `GenerateFn` and `NextTier` are function fields, so filewriter has no import dependency on modelmanager/router and tests use fakes. Escalation stays authoritative with the router (`NextTierUp` is a read-only view of the existing cascade).
3. **Metrics behind an interface.** `filewriter.Metrics` avoids an import cycle; the concrete adapter lives in `internal/telemetry` next to the instruments. Nil-safe so unit tests need no telemetry.
4. **Validate-total-before-mutate.** Every path in a batch is resolved and checked before the first byte is written — the HIGH-risk traversal control. Then snapshot → stage temps → swap → rollback from in-memory originals. Serial-per-worktree assumption documented (concurrency deferred, LOW).
5. **Diff is scoped to the batch, not the worktree.** The `/interpret` consumer and the retry loop see exactly this task's effect, nothing else in the worktree.
6. **Gateway never touches git.** Plain writes + in-process diff; staging/commit belong to the Mastra workflow (Q4 resolution).

## Libraries and versions

- Go 1.26 (existing toolchain; no language/toolchain bump).
- `github.com/pmezard/go-difflib v1.0.0` — NEW, the only new dependency. Pure Go, zero transitive deps, MIT.
- OpenTelemetry metrics/tracing via the existing `internal/telemetry` setup — no version change.
- Tests: stdlib `testing` + `t.TempDir` worktrees; `otel/sdk/metric` manual reader and `otel/sdk/trace` tracetest where `internal/telemetry` tests already use them. No new test dependencies.

## Testing strategy

- One Go test function per spec scenario ID (PARSE-*, SAFE-*, APPLY-*, RETRY-*, DIFF-*, HANDLER-*, TELEM-*, PROMPT-*); see mapping table in tasks.md.
- Fakes: `GenerateFn` recording `(tier, promptVariant)` calls; temp worktrees via `t.TempDir`; httptest servers for REST/MCP/remote.
- Atomicity tests assert byte-identical worktree state and absence of `.gw-tmp-*` leftovers after failure.
- Telemetry tests use the in-memory meter/tracer providers already set up in `internal/telemetry` tests.

## Deferred (named, not designed)

- Concurrent batches per worktree (LOW — pipeline is serial today).
- Per-task-size choice between full replacement and patching (RD-1 fixes full replacement for v0.4).
- Token-cost-per-artifact refinements beyond existing cost instruments.
