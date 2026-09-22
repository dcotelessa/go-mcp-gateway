# Tasks: v0.4 File Writing — Closing the Implementation Loop

Conventions:
- Each implementation task is immediately followed by its paired test task.
- Complexity: `scaffold` / `single_file` / `multi_file` / `text_op`. Within each group: scaffold → single_file → multi_file.
- Every test task implements one Go test function per spec scenario ID.
- All tasks stay within existing packages plus the new `internal/filewriter`.
- Amendments AM-1..3 are recorded in design.md § Amendments.

## Scenario → test-task map

| Spec scenarios | Test task |
|---|---|
| PARSE-10 | 1.2 |
| PARSE-1..9, PARSE-11 | 1.4 / 1.6 |
| SAFE-1..9 | 2.2 |
| APPLY-6, APPLY-7 | 3.2 |
| APPLY-1..5, APPLY-8 | 3.4 |
| RETRY-5 (errors.Is) | 4.2 |
| RETRY-7 (NextTierUp) | 4.4 |
| RETRY-1..8 | 4.6 |
| DIFF-1..7 | 5.2 |
| TELEM-1..3, 5, 6 | 6.2 |
| TELEM-4 | 6.4 |
| PROMPT-1, 4 | 7.2 |
| PROMPT-2, 3 | 7.4 |
| HANDLER-1, 2, 3, 6, 7 | 8.2 |
| HANDLER-4 | 8.4 |
| HANDLER-5 | 8.6 |
| end-to-end regression | 9.2 |

---

## Group 1 — Parsing (`specs/filewriter-parsing.md`)

- [x] 1.1 (scaffold, impl) `internal/filewriter/types.go`: `OpKind`, `FileOp`, `Change`, `ParseError` with reason constants and `Error()`.
- [x] 1.2 (single_file, test) `types_test.go`: `TestParseError_MessageContainsReasonAndDetail` — PARSE-10.
- [x] 1.3 (single_file, impl) `parser.go`: fence scanner recognizing `file:` / `file-delete:`, ordered ops, content fidelity, parse errors.
- [x] 1.4 (single_file, test) `parser_test.go`: PARSE-1, 3, 4, 5, 6, 8, 9.
- [x] 1.5 (single_file, impl) Amendments AM-1 and AM-3: add `OpWrite`; remove `ReasonEmptyContent`; add `Change.Skipped`; make the parser pure (`NewParser()` takes no worktree, no filesystem access, `file:` blocks emit `OpWrite`, empty bodies accepted).
- [x] 1.6 (single_file, test) `parser_test.go` updates: `TestParse_FileBlocksAreUnclassifiedWrites` (PARSE-2), `TestParse_EmptyBodyIsEmptyFile` (PARSE-7), `TestParse_BackticksInsideNonOpFenceIgnored` (PARSE-11); remove temp-directory helpers.

## Group 2 — Path safety (`specs/filewriter-path-safety.md`)

- [ ] 2.1 (single_file, impl) `pathsafe.go`: `SafetyConfig`, `NewPathValidator` (root must be an existing directory), `Resolve(raw)` doing Clean + EvalSymlinks + within-root verification + `.gitignore` match + binary-extension rejection; fail closed on ambiguity.
- [ ] 2.2 (single_file, test) `pathsafe_test.go`: SAFE-1..9. SAFE-1/2/3 also assert nothing was created on disk.

## Group 3 — Atomic apply (`specs/filewriter-atomic-apply.md`)

- [ ] 3.1 (scaffold, impl) `writer.go`: `Writer` (validator + config), `WriteError` reasons (`not_regular_file`, `io_error`), `Plan`/`DryRun` — validation plus create/modify classification against the resolved path (AM-3), zero mutation.
- [ ] 3.2 (single_file, test) `writer_plan_test.go`: `TestDryRun_ReturnsPlanWithoutMutation` (APPLY-6), `TestWriter_ClassifiesCreateVsModify` (APPLY-7).
- [ ] 3.3 (multi_file, impl) `apply.go`: `Writer.Apply` — validate+classify all → snapshot originals → stage `.gw-tmp-*` siblings → rename-swap (writes first, deletes last, parents auto-created) → rollback from snapshots on any failure, temp files removed on every exit path. Deleting an absent file yields a `Skipped` change, not an error (AM-2).
- [ ] 3.4 (multi_file, test) `apply_test.go`: `TestApply_BatchSucceedsFully` (APPLY-1), `TestApply_RollbackByteIdentical` (APPLY-2), `TestApply_CreatesParentDirs` (APPLY-3), `TestApply_DeletesDeferredUntilSuccess` (APPLY-4), `TestApply_DeleteMissingIsSkipped` (APPLY-5), `TestApply_EmptyFileWrite` (APPLY-8). APPLY-2/4 assert no `.gw-tmp-*` files remain.

## Group 4 — Retry & escalation executor (`specs/retry-escalation.md`)

- [ ] 4.1 (scaffold, impl) `executor.go` shell: `GenerateFn`, `Input`, `Result`, `Metrics` interface (including `PhantomDelete`), `ErrParseExhausted`, `NewExecutor`.
- [ ] 4.2 (single_file, test) `TestErrParseExhausted_WrapsLastParseError` — RETRY-5.
- [ ] 4.3 (single_file, impl) `internal/router/nexttier.go`: additive `NextTierUp(tier) (string, bool)` derived from the existing cascade ordering.
- [ ] 4.4 (single_file, test) `TestNextTierUp_MiddleTier`, `TestNextTierUp_TopTierHasNoNext` — RETRY-7 precondition.
- [ ] 4.5 (multi_file, impl) `Executor.Run`: the RD-2 ladder, strict-prompt selection, Metrics hooks, terminal `write_failed`, Result.Tier/Attempts.
- [ ] 4.6 (multi_file, test) `executor_run_test.go`: RETRY-1..8.

## Group 5 — Diff generation (`specs/diff-generation.md`)

- [ ] 5.1 (single_file, impl) Add `github.com/pmezard/go-difflib v1.0.0`; `diff.go`: `Batch.Diff(contextLines)` over applied, non-skipped changes in batch order; `/dev/null` headers for create/delete; deterministic; "" when nothing changed.
- [ ] 5.2 (single_file, test) `diff_test.go`: DIFF-1..7.

## Group 6 — Telemetry (`specs/telemetry.md`)

- [ ] 6.1 (scaffold, impl) `internal/telemetry`: register `FilesWritten`, `WriteFailures`, `ParseFailures`, `PhantomDeletes` counters; `FilewriterMetrics` adapter implementing `filewriter.Metrics` (nil-safe).
- [ ] 6.2 (single_file, test) `filewriter_metrics_test.go`: TELEM-1, 2, 3, 5, 6.
- [ ] 6.3 (multi_file, impl) Wire metrics and tracing through the pipeline: parse failures per attempt, files_written and `file.written` events per applied change, phantom deletes per skipped delete, write failure reasons. No content in any attribute.
- [ ] 6.4 (multi_file, test) `executor_telemetry_test.go`: TELEM-4.

## Group 7 — Prompt contract (`specs/prompt-contract.md`)

- [ ] 7.1 (single_file, impl) `prompt.go`: standard template — fence contract including an explicit instruction to close every block with ``` on its own line, a minimal valid example, full-replacement requirement, no-prose constraint; renders task and graft context; omits empty context.
- [ ] 7.2 (single_file, test) PROMPT-1, PROMPT-4.
- [ ] 7.3 (multi_file, impl) Strict variant and `FileWriterConfig{BinaryExtensions, DiffContextLines, PromptOverrides}`; per-tier overrides.
- [ ] 7.4 (multi_file, test) PROMPT-2, PROMPT-3, `TestFileWriterConfig_Defaults`.

## Group 8 — Handler integration (`specs/handler-integration.md`)

- [ ] 8.1 (multi_file, impl) `internal/rest` `/implement`: construct the pipeline per request from `worktreePath`; wire `GenerateFn` and `NextTier`; map Result → `files_changed` (non-skipped only) and `diff`; failures → `reason` + `attempts`.
- [ ] 8.2 (multi_file, test) HANDLER-1, 2, 3, 6, 7.
- [ ] 8.3 (multi_file, impl) `internal/mcp` `route_complete`: same pipeline and mapping.
- [ ] 8.4 (multi_file, test) HANDLER-4.
- [ ] 8.5 (single_file, impl) `internal/remote`: `diff` in the `/interpret` payload.
- [ ] 8.6 (single_file, test) HANDLER-5.

## Group 9 — Wiring & end-to-end verification

- [ ] 9.1 (text_op, impl) `cmd/gateway`: register `FileWriterConfig` defaults, inject `FilewriterMetrics`, document new config keys.
- [ ] 9.2 (multi_file, test) `implement_e2e_test.go`: fake model emitting one create, one modify, one delete of an absent file; asserts files on disk, `files_changed`, `diff`, counters including phantom_deletes, and the `/interpret` payload diff.
