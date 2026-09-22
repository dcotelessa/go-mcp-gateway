# Spec: Telemetry for File Writing

Covers: new instruments in `internal/telemetry` and their emission points. Existing instruments are unchanged.
Amended by **AM-1** (no `empty_content`) and **AM-2** (skipped deletes are counted, not failed).

## Requirements

### Requirement: files_written counter
The gateway SHALL expose `gateway.task.files_written` (Int64Counter) and increment it once per file actually written or deleted, with attributes `tier`, `complexity`, `operation`. Skipped deletes SHALL NOT increment it.

### Requirement: write_failures counter
The gateway SHALL expose `gateway.task.write_failures` (Int64Counter) and increment it once per rejected or failed operation, with attribute `reason` (`path_traversal`, `gitignored`, `binary_extension`, `not_regular_file`, `io_error`).

### Requirement: parse_failures counter
The gateway SHALL expose `gateway.task.parse_failures` (Int64Counter) and increment it once per parse-failed model attempt (including retries and escalation attempts), with attributes `reason` (`no_operations`, `unclosed_fence`, `missing_path`, `duplicate_path`) and `tier`.

### Requirement: phantom_deletes counter
The gateway SHALL expose `gateway.task.phantom_deletes` (Int64Counter) and increment it once per skipped delete, with attribute `tier`. It is a signal of possible codebase hallucination, not an error.

### Requirement: Span events
The implement span SHALL record one `file.written` event per applied, non-skipped change with attributes `path`, `operation`, `bytes`.

### Requirement: No content in telemetry
Span attributes and events SHALL NOT contain file content, prompt text, or graft context — paths, operations, sizes, and reasons only.

### Requirement: Existing instruments unchanged
All pre-existing counters, histograms, and spans SHALL keep their names, attributes, and emission points.

## Scenarios

### TELEM-1 — files_written per file with attributes
GIVEN a successful run that wrote one create and one modify
WHEN counters are inspected
THEN `gateway.task.files_written` increased by 2 with one attribute set per operation

### TELEM-2 — write_failures with reason
GIVEN an op rejected as `path_traversal`
WHEN counters are inspected
THEN `gateway.task.write_failures` increased by 1 with attribute reason=path_traversal

### TELEM-3 — parse_failures per failed attempt
GIVEN a run with one parse failure before a successful retry
WHEN counters are inspected
THEN `gateway.task.parse_failures` increased by 1 with the ParseError reason and the attempt's tier

### TELEM-4 — Span events per file
GIVEN a successful run that wrote two files
WHEN the implement span is inspected
THEN it contains two `file.written` events with attributes path, operation, and bytes, and no content

### TELEM-5 — No writes, no counter movement
GIVEN a run that exhausted parse attempts
WHEN counters are inspected
THEN `gateway.task.files_written` is unchanged and `gateway.task.parse_failures` increased by the number of failed attempts

### TELEM-6 — Skipped delete counted as phantom, not written
GIVEN a successful run containing one skipped delete
WHEN counters are inspected
THEN `gateway.task.phantom_deletes` increased by 1, `gateway.task.files_written` did not count it, and no `file.written` event was recorded for it
