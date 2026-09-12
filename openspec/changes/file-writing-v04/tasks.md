# Tasks: v0.4 File Writing — Closing the Implementation Loop

Conventions:
- Each implementation task is immediately followed by its paired test task.
- Complexity: `scaffold` / `single_file` / `multi_file` / `text_op`. Within each group: scaffold → single_file → multi_file.
- Every test task implements one Go test function per spec scenario ID; scenario IDs are defined in `specs/*.md`.
- All tasks stay within existing packages plus the new `internal/filewriter`.

## Scenario → test-task map

| Spec scenarios | Test task |
|---|---|
| PARSE-1..9 | 1.4 |
| PARSE-10 | 1.2 |
| SAFE-1..9 | 2.2 |
| APPLY-6 | 3.2 |
| APPLY-1..5 | 3.4 |
| RETRY-5 (errors.Is) | 4.2 |
| RETRY-7 (NextTierUp) | 4.4 |
| RETRY-1..8 | 4.6 |
| DIFF-1..5 | 5.2 |
| TELEM-1..3, 5 | 6.2 |
| TELEM-4 | 6.4 |
| PROMPT-1, 4 | 7.2 |
| PROMPT-2, 3 | 7.4 |
| HANDLER-1, 2, 3, 6 | 8.2 |
| HANDLER-4 | 8.4 |
| HANDLER-5 | 8.6 |
| end-to-end (HANDLER-1..5 regression) | 9.2 |

---

## Group 1 — Parsing (`specs/filewriter-parsing.md`)

- [ ] 1.1 (scaffold, impl) Create `internal/filewriter` package with `types.go`: `OpKind` constants, `FileOp`, `Change`, `ParseError` with reason constants (`no_operations`, `unclosed_fence`, `missing_path`, `empty_content`, `duplicate_path`) and `Error()`.
- [ ] 1.2 (single_file, test) `filewriter/types_test.go`: `TestParseError_MessageContainsReasonAndDetail` — PARSE-10.
- [ ] 1.3 (single_file, impl) `filewriter/parser.go`: fence scanner recognizing `file:` / `file-delete:` info strings, existence-based kind classification against the worktree, ordered ops, content fidelity, and all five `ParseError` cases including `duplicate_path`.
- [ ] 1.4 (single_file, test) `filewriter/parser_test.go` (t.TempDir worktrees): `TestParse_MultipleBlocksInOrder` (PARSE-1), `TestParse_KindClassification` (PARSE-2), `TestParse_DeleteBlock` (PARSE-3), `TestParse_NoOperations` (PARSE-4), `TestParse_UnclosedFence` (PARSE-5), `TestParse_MissingPath` (PARSE-6), `TestParse_EmptyContent` (PARSE-7), `TestParse_IgnoresNonFileFences` (PARSE-8), `TestParse_DuplicatePath` (PARSE-9).

## Group 2 — Path safety (`specs/filewriter-path-safety.md`)

- [ ] 2.1 (single_file, impl) `filewriter/pathsafe.go`: `SafetyConfig` (binary extension list), `NewPathValidator` (root must be an existing directory), `Resolve(raw)` doing Clean + EvalSymlinks + within-root verification + `.gitignore` match + binary-extension rejection; fail closed on ambiguity.
- [ ] 2.2 (single_file, test) `filewriter/pathsafe_test.go`: `TestResolve_RejectsRelativeTraversal` (SAFE-1), `TestResolve_RejectsOutsideAbsolute` (SAFE-2), `TestResolve_RejectsPrefixTraversal` (SAFE-3), `TestResolve_AcceptsInsideAbsolute` (SAFE-4), `TestResolve_RejectsGitignored` (SAFE-5), `TestResolve_RejectsBinaryExtension` (SAFE-6), `TestResolve_RejectsSymlinkEscape` (SAFE-7), `TestResolve_ValidRelativePath` (SAFE-8), `TestNewPathValidator_InvalidRoot` (SAFE-9). SAFE-1/2/3 also assert nothing was created on disk.

## Group 3 — Atomic apply (`specs/filewriter-atomic-apply.md`)

- [ ] 3.1 (scaffold, impl) `filewriter/writer.go`: `Writer` struct (validator + config), `WriteError` reasons (`not_regular_file`, `delete_missing`, `io_error`), `Plan`/`DryRun` validation-only path returning planned `Change`s with zero mutation.
- [ ] 3.2 (single_file, test) `filewriter/writer_plan_test.go`: `TestDryRun_ReturnsPlanWithoutMutation` (APPLY-6) — asserts byte-identical worktree before/after.
- [ ] 3.3 (multi_file, impl) `filewriter/apply.go`: `Writer.Apply` — validate all → snapshot originals → stage `.gw-tmp-*` siblings → rename-swap (creates/modifies first, deletes last, parents auto-created) → rollback from snapshots on any failure, remove temp files on every exit path; return `*Batch` with Changes and before/after snapshots.
- [ ] 3.4 (multi_file, test) `filewriter/apply_test.go`: `TestApply_BatchSucceedsFully` (APPLY-1), `TestApply_RollbackByteIdentical` (APPLY-2), `TestApply_CreatesParentDirs` (APPLY-3), `TestApply_DeletesDeferredUntilSuccess` (APPLY-4), `TestApply_DeleteMissingRollsBack` (APPLY-5). APPLY-2/4/5 assert no `.gw-tmp-*` files remain.

## Group 4 — Retry & escalation executor (`specs/retry-escalation.md`)

- [ ] 4.1 (scaffold, impl) `filewriter/executor.go` shell: `GenerateFn`, `Input`, `Result`, `Metrics` interface, `ErrParseExhausted`, `NewExecutor` wiring Parser/Writer/Prompts.
- [ ] 4.2 (single_file, test) `filewriter/executor_test.go`: `TestErrParseExhausted_WrapsLastParseError` — RETRY-5 (errors.Is/As contract, empty Changes/Diff).
- [ ] 4.3 (single_file, impl) `internal/router/nexttier.go`: additive `NextTierUp(tier) (string, bool)` derived from the existing cascade ordering; no change to selection/fallback logic.
- [ ] 4.4 (single_file, test) `internal/router/nexttier_test.go`: `TestNextTierUp_MiddleTier` and `TestNextTierUp_TopTierHasNoNext` — RETRY-7 (ladder-termination precondition).
- [ ] 4.5 (multi_file, impl) `filewriter/executor.go` `Run`: the RD-2 attempt ladder (standard → strict → next-tier strict), stop on first parse success, strict-prompt selection via PromptRenderer, Metrics hooks for parse failures, terminal `write_failed` on apply errors, Result.Tier/Attempts.
- [ ] 4.6 (multi_file, test) `filewriter/executor_run_test.go` (recording fake GenerateFn, temp worktree): `TestRun_RetriesSameTierWithStrictPrompt` (RETRY-1), `TestRun_SameTierRetryAppliesFiles` (RETRY-2), `TestRun_EscalatesAfterSecondFailure` (RETRY-3), `TestRun_EscalatedSuccessRecordsTier` (RETRY-4), `TestRun_AllAttemptsFailExhaustion` (RETRY-5), `TestRun_CleanParseSingleCall` (RETRY-6), `TestRun_TopTierTwoAttemptsOnly` (RETRY-7), `TestRun_WriteFailureTerminalNoReprompt` (RETRY-8).

## Group 5 — Diff generation (`specs/diff-generation.md`)

- [ ] 5.1 (single_file, impl) Add `github.com/pmezard/go-difflib v1.0.0` to `go.mod` and implement `filewriter/diff.go`: `Batch.Diff(contextLines)` — unified diff over touched files in batch order, `a/`·`b/` headers, `/dev/null` for create/delete, deterministic (no timestamps), "" for empty batch.
- [ ] 5.2 (single_file, test) `filewriter/diff_test.go`: `TestDiff_ModifyShowsHunk` (DIFF-1), `TestDiff_CreateAllAdditions` (DIFF-2), `TestDiff_DeleteAllDeletions` (DIFF-3), `TestDiff_EmptyWhenNoWrites` (DIFF-4), `TestDiff_NoOpModifyNoHunks` (DIFF-5).

## Group 6 — Telemetry (`specs/telemetry.md`)

- [ ] 6.1 (scaffold, impl) `internal/telemetry`: register `FilesWritten`, `WriteFailures`, `ParseFailures` Int64Counters (`gateway.task.*`) on the existing instrument set; add `FilewriterMetrics` adapter implementing `filewriter.Metrics` (nil-safe when providers are no-op).
- [ ] 6.2 (single_file, test) `internal/telemetry/filewriter_metrics_test.go` (manual meter reader): `TestFilesWrittenCounterAttributes` (TELEM-1), `TestWriteFailuresReasonAttr` (TELEM-2), `TestParseFailuresReasonAndTier` (TELEM-3), `TestNoWritesCounterUnchanged` (TELEM-5).
- [ ] 6.3 (multi_file, impl) Wire metrics + tracing through the pipeline: executor emits `ParseFailure` per failed attempt and `FilesWritten` + `SpanFileWritten` per change on the implement span (`file.written`, attrs path/operation/bytes); writer surfaces per-op failure reasons for `WriteFailure`.
- [ ] 6.4 (multi_file, test) `filewriter/executor_telemetry_test.go` (tracer/meter fakes): `TestRun_SpanEventPerWrittenFile` (TELEM-4).

## Group 7 — Prompt contract (`specs/prompt-contract.md`)

- [ ] 7.1 (single_file, impl) `filewriter/prompt.go`: `PromptRenderer` with embedded standard template — fence contract + minimal valid example + full-replacement requirement + no-prose constraint; renders task description and graft context; omits empty context section.
- [ ] 7.2 (single_file, test) `filewriter/prompt_test.go`: `TestStandardPrompt_CarriesContract` (PROMPT-1), `TestStandardPrompt_EmptyGraftOmitted` (PROMPT-4).
- [ ] 7.3 (multi_file, impl) Add strict variant to `filewriter/prompt.go` (only file blocks + reproduce ENTIRE file) and `FileWriterConfig{BinaryExtensions, DiffContextLines, PromptOverrides}` to `internal/config`; renderer applies per-tier overrides.
- [ ] 7.4 (multi_file, test) `filewriter/prompt_test.go` additions + `internal/config` defaults check: `TestStrictPrompt_TightensContract` (PROMPT-2), `TestPrompt_PerTierOverrideWins` (PROMPT-3), `TestFileWriterConfig_Defaults` (binary extension list, DiffContextLines=3).

## Group 8 — Handler integration (`specs/handler-integration.md`)

- [ ] 8.1 (multi_file, impl) `internal/rest` `/implement` handler: construct Parser/PathValidator/Writer/Executor per request from `worktreePath`, wire GenerateFn to the existing model-call path and NextTier to `router.NextTierUp`, attach `FilewriterMetrics`; map Result → response (`files_changed`, `diff`) and failures → `reason` `parse_failed`/`write_failed` + `attempts`; keep all v0.3 fields.
- [ ] 8.2 (multi_file, test) `internal/rest/implement_test.go` (httptest, fake GenerateFn, t.TempDir worktree): `TestImplement_WritesRealFilesAndReturnsChanges` (HANDLER-1), `TestImplement_ResponseContainsDiff` (HANDLER-2), `TestImplement_ParseExhaustionStructuredFailure` (HANDLER-3), `TestImplement_ResponseContractAdditive` (HANDLER-6).
- [ ] 8.3 (multi_file, impl) `internal/mcp` `route_complete` handler: same executor construction and result mapping; tool result includes `files_changed` and `diff`.
- [ ] 8.4 (multi_file, test) `internal/mcp/route_complete_test.go`: `TestRouteComplete_SameWritePath` (HANDLER-4).
- [ ] 8.5 (single_file, impl) `internal/remote`: add `diff` to the `/interpret` request payload, populated from the run's diff on success, `""` otherwise.
- [ ] 8.6 (single_file, test) `internal/remote/interpret_test.go` (httptest server asserting payload): `TestInterpret_PayloadCarriesDiff` (HANDLER-5).

## Group 9 — Wiring & end-to-end verification

- [ ] 9.1 (text_op, impl) `cmd/gateway`: register `FileWriterConfig` defaults (binary extension list, DiffContextLines=3), instantiate and inject `FilewriterMetrics`, and document the new config keys alongside existing configuration.
- [ ] 9.2 (multi_file, test) `internal/rest/implement_e2e_test.go`: full pipeline regression — fake model emitting one create + one modify; asserts files on disk, response `files_changed`/`diff`, telemetry counters moved, and `/interpret` payload diff non-empty (integration coverage of HANDLER-1..5, TELEM-1/4, DIFF-1/2).
