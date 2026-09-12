# Proposal: v0.4 File Writing — Closing the Implementation Loop

## Problem

The gateway classifies tasks, routes them to the right model tier, enriches
prompts with graft codebase context, and measures cost per call. What it does
not do is produce artifacts.

Today `/implement` returns the model's response in a `content` string. Nothing
writes that content to disk. Consequences:

- `files_changed` is echoed from the request, never populated from real writes
- QA runs `vitest` against an unchanged worktree, so results are meaningless
- The retry loop has no diff to reason about — a failed task retries against
  identical state and produces identical output
- `git commit` in the Mastra workflow commits nothing but a checked-off
  checkbox in tasks.md
- Cost-per-accepted-artifact measures cost per *response*, not per artifact,
  because no artifact is created

This is the gap between a routing and measurement layer and a pipeline that
builds software. Until an agent can write a file, LSP diagnostics have nothing
to diagnose and the QA loop cannot converge.

## What Will Change

### File writing
- ADDED: `internal/filewriter` package — parses model output into file operations
- ADDED: Structured output contract — implement prompts instruct the model to
  emit fenced blocks with explicit file paths
- ADDED: Path safety — all writes confined to the task's `worktreePath`;
  traversal outside it is rejected before any disk operation
- ADDED: Operation types — create, modify (full replacement), delete
- ADDED: Atomic batch semantics — all operations in a response succeed or none
  are applied
- MODIFIED: REST `/implement` handler — writes parsed files, returns real
  `files_changed`
- MODIFIED: MCP `route_complete` handler — same write path

### Diff generation
- ADDED: Unified diff of the worktree after writes, returned in the response
- MODIFIED: Mastra `/interpret` call — receives the real diff instead of `""`

### Telemetry
- ADDED: `gateway.task.files_written` counter, attributes: tier, complexity, operation
- ADDED: `gateway.task.write_failures` counter, attributes: reason
- ADDED: `gateway.task.parse_failures` counter — model output that yielded no
  parseable file operations
- ADDED: Span events on the implement span for each file written

### Prompt contract
- ADDED: System prompt template instructing structured file output
- ADDED: Per-tier prompt variants if parse reliability differs by model

## What Won't Change

- REST and MCP request/response contract shapes (fields added, none removed)
- `reasoning_tags` contract with the Mastra workflow
- Routing logic, fallback cascade, tier selection
- Graft enrichment behaviour
- Policy and budget enforcement
- Existing telemetry instruments

## Key Risks and Dependencies

- (HIGH) Path traversal — a model emitting `../../etc/passwd` or an absolute
  path must be rejected before any filesystem call. Every path resolved and
  verified to be within `worktreePath`.
- (HIGH) Parse reliability — models do not reliably emit clean fenced blocks
  with paths. A parse failure must be a structured error the retry loop can
  act on, not a silent no-op.
- (MEDIUM) Partial write failure — three files specified, the second fails.
  Requires either atomic staging or explicit rollback.
- (MEDIUM) Full replacement vs patch — replacing whole files is simpler to
  implement and parse, but wasteful on large files and risks the model
  reproducing unchanged code incorrectly.
- (MEDIUM) Token cost of full-file output — a model rewriting a 500-line file
  spends significant output tokens. Interacts directly with the
  cost-per-artifact metrics already in place.
- (LOW) Binary and generated files — writes should refuse paths matching
  gitignore patterns or known binary extensions.
- (LOW) Concurrent writes — two tasks in the same worktree. Current pipeline
  is serial per worktree, so deferred, but worth naming.

## Decisions

All open questions are resolved. See `design.md` for the full rationale.

| # | Question | Decision |
|---|----------|----------|
| RD-1 | Replacement, patches, or search/replace? | Full file replacement |
| RD-2 | Parse failure: retry or escalate? | Retry once same tier with strict prompt, then escalate one tier |
| Q3 | Staged temp dir or direct write + rollback? | Sibling temp file + rename-swap; in-memory snapshot for rollback |
| Q4 | Does the gateway run `git add`? | No. Plain writes only; staging/commit stay in the Mastra workflow |
| Q5 | Dry-run mode? | Yes, library API only (`Writer.DryRun`); not on the REST/MCP contract |
