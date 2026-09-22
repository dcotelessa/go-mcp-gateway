# Design: v0.4 File Writing — Closing the Implementation Loop

## Research decisions implemented (binding)

| # | Decision | How this design applies it |
|---|----------|----------------------------|
| RD-1 | Full file replacement. Not unified diff patches, not search/replace blocks. | The output contract is whole-file bodies in `file:` fences. The parser never merges fragments; the writer never patches. `modify` means "replace entire file". |
| RD-2 | Parse failure: retry once on the same tier with a stricter prompt, then escalate to the next tier up. | The executor implements a fixed ladder: `(tier, standard)` → `(tier, strict)` → `(nextTier, strict)`, max 3 model calls, stop on first parse success. Recovery is driven by the structured `*ParseError`, never string sniffing. Write failures are terminal and never re-prompted. |

### Open questions resolved for v0.4 (binding, do not revisit)

| Open question | Resolution |
|---|---|
| Q3: staged temp dir vs direct write + rollback | Stage each new content to a sibling temp file (`.gw-tmp-*`) in the target directory (same filesystem), then rename-swap into place. Originals are snapshotted in memory before swapping for rollback. |
| Q4: does the gateway run `git add`? | No. Plain filesystem writes only. Staging and commit remain the Mastra workflow's job. The git index is never touched. |
| Q5: dry-run mode | Yes, as a library API (`Writer.DryRun`). Not exposed on the REST/MCP contract in v0.4. |
| Diff implementation | In-process unified diff via `github.com/pmezard/go-difflib v1.0.0`. No `git diff` subprocess, deterministic headers. |

## Amendments (design review, applied before Group 3)

A design review against deep-module principles (see `openspec/design-principles.md`) found three places where the original design added error paths or coupling without adding capability. Each amendment removes something.

| # | Change | Rationale |
|---|--------|-----------|
| AM-1 | Remove `empty_content`. An empty `file:` body writes an empty file. | An empty file is legitimate (package markers, fixtures). The error rejected valid output and would have triggered the retry ladder — burning model calls — on a correct response. Defines an error out of existence. |
| AM-2 | Delete is idempotent. Deleting an absent file succeeds with `Change.Skipped = true`; `delete_missing` is removed. | The desired end state — file absent — already holds. The old error triggered a full batch rollback over a no-op. The information is preserved as the `gateway.task.phantom_deletes` signal, since it may indicate a model hallucinated the codebase. |
| AM-3 | The parser is pure. It emits `OpWrite`; the writer classifies create vs modify during validation. | The original parser stat'd the worktree while the path validator separately resolved paths against it — two modules each knowing how worktree resolution works (information leakage). Classification now happens against the validator's resolved path, in one place, so the two can never disagree. The parser needs no filesystem and its tests need no temp directories. |

## Technical approach

```
POST /implement  (or MCP route_complete)
→ policy/budget check            (unchanged, internal/policy)
→ tier selection                 (unchanged, internal/router)
→ graft enrichment               (unchanged, internal/graft)
→ filewriter.Executor.Run:
    ladder = [(T, standard), (T, strict), (next(T), strict)]
    for each (tier, prompt) in ladder:
        out   = Generate(tier, prompt)       // injected; calls the model path
        ops   = Parser.Parse(out)            // pure; *ParseError → next rung
        batch = Writer.Apply(ctx, ops)       // validate + classify ALL first
                                             // any WriteError → TERMINAL
        diff  = batch.Diff()
    → files_written per applied change; phantom_deletes per skipped delete
    all rungs fail → ErrParseExhausted → handler maps to "parse_failed"
→ response: {…existing fields…, files_changed, diff}   (additive)
→ remote /interpret payload gains diff
```

## Package structure (no new top-level packages beyond internal/filewriter)

```
internal/filewriter/
  types.go      OpKind (write|create|modify|delete), FileOp, Change,
                ParseError (+ reasons), WriteError (+ reasons),
                ErrParseExhausted, Metrics interface
  parser.go     Parser.Parse(output) ([]FileOp, error)   // pure fence scanner
  pathsafe.go   PathValidator, SafetyConfig, Resolve(raw) (rel, abs, err)
  writer.go     Writer: Plan / DryRun — validation + classification
  apply.go      Writer.Apply: validate+classify → snapshot → stage temps →
                swap → rollback; idempotent deletes; Batch
  diff.go       Batch.Diff(contextLines) string          // go-difflib
  executor.go   Executor.Run: attempt ladder, Metrics hooks, Result
  prompt.go     PromptRenderer: standard + strict templates

internal/router/nexttier.go   NextTierUp(tier) (string, bool)   // additive, read-only
internal/telemetry/           +4 counters, FilewriterMetrics adapter
internal/config/              +FileWriterConfig{BinaryExtensions, DiffContextLines, PromptOverrides}
internal/rest/                /implement: build executor, map Result → response
internal/mcp/                 route_complete: same executor
internal/remote/              /interpret payload gains diff
cmd/gateway/                  register defaults + instruments
```

## Key types

```go
package filewriter

type OpKind string // "write" | "create" | "modify" | "delete"

type FileOp struct {
    Kind    OpKind // OpWrite or OpDelete only — the parser never classifies
    RawPath string // exactly as emitted
    Path    string // cleaned, worktree-relative
    Content string // complete new file; may be empty; empty for delete
}

type ParseError struct {
    Reason string // no_operations|unclosed_fence|missing_path|duplicate_path
    Detail string
    Line   int    // best-effort 1-based line of the offending fence
}

type WriteError struct {
    Reason string // path_traversal|gitignored|binary_extension|not_regular_file|io_error
    Path   string
    Err    error
}

type Change struct {
    Path      string
    Operation OpKind // create|modify|delete — never write
    Bytes     int
    Skipped   bool   // delete of an absent file: no effect, counted as phantom
}

type Batch struct {
    Changes []Change
    // unexported: before/after snapshots + order, captured by Apply
}
func (b *Batch) Diff(contextLines int) string

type GenerateFn func(ctx context.Context, tier, systemPrompt, userPrompt string) (string, error)

type Metrics interface { // adapter in internal/telemetry; nil-safe
    FilesWritten(ctx context.Context, tier, complexity, operation string)
    WriteFailure(ctx context.Context, reason string)
    ParseFailure(ctx context.Context, reason, tier string)
    PhantomDelete(ctx context.Context, tier string)
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
    Content  string
    Tier     string // tier of the ACCEPTED attempt
    Attempts int
    Changes  []Change
    Diff     string
}
```

## Output contract (normative grammar)

```
response := ( prose | fence )*
fence    := "```" info "\n" body "\n" "```"
info     := "file:" path | "file-delete:" path
body     := complete new file content      (may be empty)
```

Only `file:` / `file-delete:` fences are operations; every other fence, and anything inside it, is ignored. Two ops whose paths clean to the same relative path → `duplicate_path`. Create vs modify is decided by the writer, never the parser.

## Error taxonomy → metric mapping

| Layer | Reasons | Metric |
|---|---|---|
| Parse | no_operations, unclosed_fence, missing_path, duplicate_path | `gateway.task.parse_failures{reason, tier}`; drives the RD-2 ladder |
| Write | path_traversal, gitignored, binary_extension, not_regular_file, io_error | `gateway.task.write_failures{reason}`; terminal |
| Signal (not an error) | skipped delete | `gateway.task.phantom_deletes{tier}` |
| Handler | parse_failed, write_failed (+ attempts) | response failure mapping |

## Architectural decisions

- **filewriter owns contract + recovery; handlers stay thin.** REST and MCP behave identically because all parsing, path safety, classification, atomicity, diffing, and the RD-2 ladder live in `internal/filewriter`.
- **The parser is pure (AM-3).** Filesystem knowledge lives in the writer and validator only.
- **Model calls are injected.** `GenerateFn` and `NextTier` are function fields, so filewriter has no import dependency on modelmanager or router.
- **Metrics behind an interface.** Avoids an import cycle; nil-safe for tests.
- **Validate-and-classify-total before mutate.** Every path is resolved, checked, and classified before the first byte is written.
- **Diff is scoped to the batch,** not the worktree.
- **Gateway never touches git.**

## Libraries and versions

- Go 1.26 (existing toolchain).
- `github.com/pmezard/go-difflib v1.0.0` — the only new dependency. Pure Go, zero transitive deps, MIT.
- OpenTelemetry via the existing `internal/telemetry` setup — no version change.

## Testing strategy

- One Go test function per spec scenario ID.
- Parser tests need no filesystem (AM-3). Writer and executor tests use `t.TempDir` worktrees.
- Atomicity tests assert byte-identical worktree state and absence of `.gw-tmp-*` leftovers after failure.

## Deferred (named, not designed)

- Concurrent batches per worktree.
- Patching instead of full replacement for large files.
- Sensitivity checks on enriched prompts — split into its own change, `sensitivity-guard`, which also caps the RD-2 ladder so a sensitive task can never escalate to a remote tier.
