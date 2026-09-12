# Spec: Parse-Failure Retry and Tier Escalation

Covers: the recovery ladder for unparseable model output (`internal/filewriter` executor; additive `internal/router` helper).
Decisions applied: **RD-2** — parse failure: retry once on the same tier with a stricter prompt, then escalate to the next tier up.

## Requirements

### Requirement: Attempt ladder
On parse failure the executor SHALL make at most three model calls per implement task, in this order:
1. original tier, standard prompt
2. original tier, strict prompt
3. next tier up (per existing cascade order), strict prompt
The executor SHALL stop at the first attempt whose output parses.

### Requirement: Strict prompt on recovery attempts
Attempts 2 and 3 SHALL use the strict prompt variant (see specs/prompt-contract.md). The original tier SHALL be retried exactly once before escalation.

### Requirement: Escalation source of truth
The next tier up SHALL be derived from the router's existing cascade ordering via an additive lookup; the executor MUST NOT re-implement tier ordering and routing/fallback behavior MUST NOT change.

### Requirement: Top-tier termination
When the task already runs at the highest tier, the ladder SHALL be two attempts and SHALL NOT attempt a nonexistent tier.

### Requirement: Parse failures do not abort silently
Each parse-failed attempt SHALL be observable (parse_failures metric) and SHALL NOT be treated as a successful no-op.

### Requirement: Write failures are terminal
A batch that parses but fails to apply (any write failure) SHALL NOT trigger another model call. The executor SHALL return a terminal `write_failed` error. RD-2 covers parse failures only.

### Requirement: Exhaustion
If all attempts fail to parse, the executor SHALL return `ErrParseExhausted` wrapping the last `*ParseError`, with no files written and empty Changes/Diff.

### Requirement: Result metadata
A successful run SHALL record the tier of the ACCEPTED attempt and the total number of model calls made.

## Scenarios

### RETRY-1 — First failure retries same tier with strict prompt
GIVEN a fake model whose first response fails to parse and whose second response parses
WHEN the executor runs
THEN exactly two model calls are made, both to the original tier, and the second call used the strict prompt variant

### RETRY-2 — Same-tier retry success applies files
GIVEN the retry response parses
WHEN the executor runs
THEN files are written from the second response, Result.Tier is the original tier, and Attempts is 2

### RETRY-3 — Second failure escalates one tier
GIVEN both responses at the original tier fail to parse and the escalated tier's response parses
WHEN the executor runs
THEN a third call is made to the next tier up using the strict prompt

### RETRY-4 — Escalated success records tier
GIVEN the escalated attempt parses
WHEN the executor runs
THEN files are written, Result.Tier is the escalated tier, Attempts is 3, and parse_failures was incremented twice with tier=original

### RETRY-5 — Exhaustion returns structured error, no writes
GIVEN all attempts fail to parse
WHEN the executor runs
THEN ErrParseExhausted wrapping the last ParseError is returned (matchable via errors.Is/As), no files are written, and Changes and Diff are empty

### RETRY-6 — Clean first parse makes a single call
GIVEN the first response parses
WHEN the executor runs
THEN exactly one model call is made with the standard prompt and Attempts is 1

### RETRY-7 — Top tier has no escalation
GIVEN the task runs at the highest tier and both attempts fail to parse
WHEN the executor runs
THEN exactly two model calls are made and ErrParseExhausted is returned

### RETRY-8 — Write failures do not re-prompt
GIVEN a response that parses but contains a path rejected as `path_traversal`
WHEN the executor runs
THEN no additional model calls are made and the returned error reason is `write_failed`
