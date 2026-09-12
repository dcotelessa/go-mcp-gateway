# Spec: REST and MCP Handler Integration

Covers: wiring the file-writing pipeline into `internal/rest` `/implement`, `internal/mcp` `route_complete`, and the `internal/remote` `/interpret` call.

## Contract deltas (additive only)

- REST `/implement` response: `files_changed` is now populated from real writes as `[{path, operation}]` (worktree-relative); new `diff` string field; failure responses gain `reason` (`parse_failed` | `write_failed`) and `attempts`. No field is removed or re-meaning'd.
- MCP `route_complete` tool result: includes the same `files_changed` and `diff`.
- Mastra `/interpret` request payload: `diff` carries the generated diff (previously always `""`).

## Requirements

### Requirement: Single shared write path
Both handlers SHALL execute the identical `internal/filewriter` pipeline (parse → retry/escalate → atomic apply → diff). Handlers MUST NOT implement parsing, path validation, or writing inline.

### Requirement: Real files_changed
On success, `files_changed` SHALL contain one entry per written file with the worktree-relative path and operation (`create` | `modify` | `delete`). The echoed-from-request behavior is removed.

### Requirement: Diff in response
On success, the response `diff` field SHALL equal the batch diff. On any failure it SHALL be empty.

### Requirement: Structured failure mapping
Parse exhaustion SHALL map to failure reason `parse_failed`; an apply failure SHALL map to `write_failed`. `attempts` SHALL equal the number of model calls made. `files_changed` SHALL be empty in both cases.

### Requirement: Interpret payload
After a successful write, the gateway SHALL send the generated diff to the Mastra `/interpret` endpoint in the existing call's payload.

### Requirement: Contract compatibility
Every field present in the v0.3 request/response contracts SHALL remain with unchanged meaning; v0.4 only adds fields.

## Scenarios

### HANDLER-1 — REST /implement writes real files
GIVEN a valid implement request, a temp worktree, and a fake model response containing two `file:` blocks
WHEN POST /implement completes
THEN both files exist on disk with the emitted content and `files_changed` contains two entries with real worktree-relative paths and operations create/modify

### HANDLER-2 — REST response includes diff
WHEN POST /implement completes with writes
THEN the response `diff` field is a non-empty unified diff identical to the batch diff

### HANDLER-3 — Parse exhaustion maps to structured failure
GIVEN fake model output that never parses
WHEN POST /implement completes
THEN the failure response carries reason `parse_failed`, `files_changed` is empty, `diff` is "", and `attempts` equals the number of model calls made

### HANDLER-4 — MCP route_complete uses the same path
GIVEN a route_complete invocation with the same inputs and fake model
WHEN the handler completes
THEN the same files are written to the worktree and the tool result includes `files_changed` and `diff`

### HANDLER-5 — /interpret receives the real diff
GIVEN a successful implement run
WHEN the gateway calls the Mastra /interpret endpoint (httptest server)
THEN the request payload's `diff` field contains the generated diff

### HANDLER-6 — Response contract is additive
WHEN any /implement response is produced
THEN every v0.3 field is still present with the same meaning; only `files_changed` (now real), `diff`, and failure `reason`/`attempts` are added
