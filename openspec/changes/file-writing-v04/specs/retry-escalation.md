# Spec: Parse-Failure Retry and Tier Escalation

Covers: the recovery ladder for unparseable model output (`internal/filewriter` executor; additive `internal/router` helper).
Decisions applied: **RD-2** — parse failure: retry once on the same tier with a stricter prompt, then escalate to the next tier up.
Amended by **AM-4** (escalation has its own ladder), **AM-5** (worktree lives only in the Writer), **AM-6** (model-call failures are terminal).

## Requirements

### Requirement: Attempt ladder
On parse failure the executor SHALL make at most three model calls per implement task, in this order:
1. original tier, standard prompt
2. original tier, strict prompt
3. next tier up, strict prompt
The executor SHALL stop at the first attempt whose output parses.

### Requirement: Strict prompt on recovery attempts
Attempts 2 and 3 SHALL use the strict prompt variant. The original tier SHALL be retried exactly once before escalation.

### Requirement: Escalation ladder is its own policy
The next tier up SHALL come from `router.NextTierUp`, an explicit escalation ladder owned by the router. It MUST NOT be derived from the budget fallback chain: the fallback chain answers "what is cheaper when a budget runs out," escalation answers "what is more capable when output fails," and the two point in opposite directions. The executor MUST NOT encode tier ordering; it receives `NextTier` as an injected function.

### Requirement: Escalation never reaches Opus
The automatic ladder SHALL be `local_ornith | local_qwen → remote_deepseek → remote_glm`. `remote_glm` is the top of the automatic ladder. `remote_opus` SHALL NOT be reachable by escalation; it is used only by explicit `force_tier`. Local tiers escalate directly to a remote tier, since moving between local models is a VRAM swap rather than an increase in capability.

### Requirement: Top-tier termination
When the task already runs at the top of the ladder, or `NextTier` is nil, the ladder SHALL be two attempts.

### Requirement: Parse failures do not abort silently
Each parse-failed attempt SHALL be observable (parse_failures metric) and SHALL NOT be treated as a successful no-op.

### Requirement: Write failures are terminal
A batch that parses but fails to apply SHALL NOT trigger another model call. The executor SHALL return a `*RunError` with reason `write_failed`.

### Requirement: Model-call failures are terminal
If the model call itself fails, the executor SHALL return a `*RunError` with reason `generate_failed` and make no further calls. Retrying an unreachable or erroring model only spends more; tier fallback on upstream failure belongs to the existing routing layer, not to this ladder.

### Requirement: Exhaustion
If every attempt fails to parse, the executor SHALL return a `*RunError` with reason `parse_failed` whose cause wraps both `ErrParseExhausted` and the last `*ParseError`, so `errors.Is` and `errors.As` both work. No files are written.

### Requirement: Result metadata
A successful run SHALL record the tier of the ACCEPTED attempt and the total number of model calls made. Failed runs report the attempt count and last tier in `*RunError`.

### Requirement: Worktree is not an input
`Input` SHALL NOT carry a worktree path. The `Writer` is already bound to one; a second copy could disagree.

### Requirement: Cancellation
A cancelled context before an attempt SHALL stop the run with no further model calls.

## Scenarios

### RETRY-1 — First failure retries same tier with strict prompt
GIVEN a fake model whose first response fails to parse and whose second parses
WHEN the executor runs
THEN exactly two calls are made, both to the original tier, the second with the strict prompt

### RETRY-2 — Same-tier retry success applies files
GIVEN the retry response parses
WHEN the executor runs
THEN files are written from it, Result.Tier is the original tier, and Attempts is 2

### RETRY-3 — Second failure escalates one tier
GIVEN both original-tier responses fail and the escalated response parses
WHEN the executor runs
THEN a third call is made to the next tier up with the strict prompt

### RETRY-4 — Escalated success records tier
GIVEN the escalated attempt parses
WHEN the executor runs
THEN Result.Tier is the escalated tier, Attempts is 3, and two parse failures were recorded against the original tier

### RETRY-5 — Exhaustion returns a structured error, no writes
GIVEN every attempt fails to parse
WHEN the executor runs
THEN a `*RunError` with reason `parse_failed` is returned, `errors.Is(err, ErrParseExhausted)` and `errors.As(err, *ParseError)` both hold, and the worktree is unchanged

### RETRY-6 — Clean first parse makes a single call
GIVEN the first response parses
WHEN the executor runs
THEN exactly one call is made, with the standard prompt

### RETRY-7 — Top tier has no escalation
GIVEN a task at the top of the ladder whose responses never parse
WHEN the executor runs
THEN exactly two calls are made and the run is exhausted

### RETRY-8 — Write failures do not re-prompt
GIVEN a response that parses but targets a path outside the worktree
WHEN the executor runs
THEN one call is made, the reason is `write_failed`, and nothing is written outside the worktree

### RETRY-9 — Model-call failure is terminal
GIVEN a model call that returns an error
WHEN the executor runs
THEN one call is made and the reason is `generate_failed`

### RETRY-10 — Ladder never reaches Opus
GIVEN any starting tier
WHEN `NextTierUp` is applied repeatedly
THEN the walk terminates without ever returning `remote_opus`

### RETRY-11 — Cancelled context makes no calls
GIVEN a cancelled context
WHEN the executor runs
THEN no model call is made and an error is returned
