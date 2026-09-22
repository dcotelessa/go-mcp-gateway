# Proposal: v0.5 Decision Layer

## Problem

The gateway makes several decisions on every task, and none are made well:

- **Classify** uses keyword matching and returns a tier with no sense of how
  sure it is.
- **Interpret** string-searches test output for "failed". It cannot tell an
  implementation bug from a wrong spec from a flaky environment — and
  misreading a wrong spec as a bug is how a retry loop burns its budget on a
  task that can never pass.
- **Build order** is whatever order the planner wrote tasks in.
- **Design quality** is not checked. Specs verify behavior and tests verify
  correctness; a design full of shallow modules passes both.
- **Sensitivity** has a deterministic guard (change `sensitivity-guard`) but
  nothing catches the vague cases — personal information in prose, a password
  described in words, client data in a fixture.

Where a thinking model makes these decisions, they are folded into
generation: expensive to revisit, non-deterministic across runs, and
impossible to re-evaluate without re-planning.

## Principles

### Three layers
Every decision lands on the lowest layer that can make it reliably:
1. **Deterministic code** — free, instant, exact about what it measures
2. **Decision model** — cheap, fast, calibrated, occasionally wrong
3. **Thinking model** — reserved for generation and genuine judgment

### A budget for small models
The point of the decision layer is fast local decisions on small models
(MiniCPM5-2B, Gemma 4 12B QAT, Ornith 1.5 9B). Overloading them makes the
decisions slow and wrong. Every decision question SHALL fit this budget:

- **One concept per question.** Never "is this design good?" — instead "must
  a caller understand internals to use this interface?"
- **Small state.** One task, one interface signature, one error type, or one
  failure excerpt. Target under ~500 tokens. No question requires reading a
  whole document.
- **Few options.** At most ~6 choices, always including an abstention such as
  `needs_clarification` or `unsure`.
- **Declared model class.** Each decision states the smallest model expected
  to handle it. The eval harness tests that claim.

A decision that cannot be expressed within this budget is not a decision-model
decision. It belongs to a thinking model, invoked on demand.

## What Will Change

### Decider abstraction
- ADDED: `internal/decision` package with a `Decider` interface: state plus
  named questions in; typed answers with probabilities out
- ADDED: Local implementation against the `parallel-decision` llama-server
  fork's `/v1/decision`, batching up to 256 contexts per request
- ADDED: Remote implementation against the Jev API
- ADDED: Per-decision backend selection, with local-only pinning

### Decision 1 — Classify
- Choice over complexity and tier with probabilities; `needs_clarification`
- Confidence bands: high routes automatically, medium escalates one tier,
  low surfaces for review
- Runs at plan time over a whole tasks.md in one batched call; dispatch
  becomes a lookup
- Expected smallest model: 2B

### Decision 2 — Failure diagnosis
- LAYER 1: pass/fail from exit codes and structured reporters
  (`vitest --reporter=json`, `go test -json`)
- LAYER 2, on failure only: choice over `implementation_bug` / `spec_wrong`
  / `environment_flake` / `unsure`, given one failing test's excerpt
- Expected smallest model: 12B

### Decision 3 — Dependency ordering
- LAYER 1: structural edges from the Go import graph and the graft index
- LAYER 2: one yes/no question per task pair, batched
- Code performs the topological sort and cycle detection
- Uncertain edges default to "depends"
- Expected smallest model: 2B

### Decision 4 — Sensitivity (model layer)
- The deterministic layer ships separately in `sensitivity-guard`
- LAYER 2, shadow mode first: yes/no questions for credentials described in
  prose, personal information about real people, and client-proprietary data
- Asked per chunk of the enriched prompt, not over the whole prompt
- Hard-pinned to local backends — never evaluated remotely
- Expected smallest model: 12B

### Decision 5 — Design review
- LAYER 0, prevention: `openspec/design-principles.md` is referenced by the
  planning agent's instructions
- LAYER 1, post-implementation, report-only: AST metrics — pass-through
  functions, exported surface vs. implementation size, parameter counts,
  exported error types per package
- LAYER 2, pre-implementation: for each interface signature in design.md,
  - yes/no: "must a caller understand this module's internals to use it?"
  - yes/no: "is this method a pass-through to another layer?"
- LAYER 2, for each error reason in design.md:
  - yes/no: "could this error be removed by changing the operation's
    definition, without losing information a caller must act on?"
- Holistic critique ("design it twice") is a thinking-model task, on demand
  only
- Advisory: findings appear at the human review gate and never block
- Calibration labels come from the author's own code, marked deep or shallow
- Expected smallest model: 12B. The 2B model is expected to fail here; the
  harness should confirm rather than assume it.

### Shadow mode and evaluation
- Decisions 1–3 and 5 record answers as span attributes while existing logic
  keeps authority
- `scripts/extract_eval.py` builds labeled classify data from tasks.md files
- `cmd/classify-eval` measures the keyword baseline
- The harness reports accuracy, latency, and calibration per model, per
  decision

### Telemetry
- ADDED: span attributes `gateway.decision.{type,backend,model,answer,probability,agreed_with_baseline}`
- ADDED: `gateway.decision.latency` histogram, attributes: type, backend, model
- EXTENDED: `gateway.sensitivity.flagged` gains `layer=2`

## What Won't Change

- Routing authority stays with the existing classifier until a decision type
  meets its promotion criteria
- REST and MCP contracts — fields added, none removed
- The v0.4 filewriter pipeline and the `sensitivity-guard` deterministic layer
- Tasks still execute serially per worktree

## Key Risks

- (HIGH) Sensitivity model false negatives — mitigated by the deterministic
  layer and workspace policy, which do not depend on the model.
- (MEDIUM) Label quality — tasks.md tags are the planner's opinion; telemetry
  outcomes should replace them.
- (MEDIUM) Label imbalance — report per-class results and calibration.
- (MEDIUM) Design review has no ground truth beyond the author's judgment.
  The rubric encodes taste; it does not measure quality objectively.
- (MEDIUM) Fork maintenance — isolate behind `Decider`; move to upstream if
  parallel decisions land there.
- (MEDIUM) MoE offload — test dense, fully resident models first.
- (MEDIUM) VRAM contention with the resident implementation model.
- (LOW) Jev API early access; pin a version.

## Open Questions

1. (HIGH) Promotion criteria. Proposed: beat baseline by 10 points and reach
   90% observed accuracy in the top confidence bucket, across at least 50
   tasks, per decision type.
2. (MEDIUM) Which local model serves decisions when the resident model is MoE?
3. (MEDIUM) Should AST metrics become gating once a baseline exists?
4. (LOW) Where do plan-time decisions live — tasks.md annotations or a
   regenerable sidecar file?

## Out of Scope

- Parallel task execution within a worktree
- Model-based redaction
- Replacing the thinking model for spec and task generation
- Holistic design scoring by small models
