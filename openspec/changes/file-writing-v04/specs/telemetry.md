# Spec: Telemetry for File Writing

Covers: new instruments in `internal/telemetry` and their emission points. Existing instruments are unchanged.

## Requirements

### Requirement: files_written counter
The gateway SHALL expose `gateway.task.files_written` (Int64Counter) and increment it once per file successfully written, with attributes `tier`, `complexity`, `operation`.

### Requirement: write_failures counter
The gateway SHALL expose `gateway.task.write_failures` (Int64Counter) and increment it once per rejected/failed operation, with attribute `reason` (`path_traversal`, `gitignored`, `binary_extension`, `not_regular_file`, `delete_missing`, `io_error`).

### Requirement: parse_failures counter
The gateway SHALL expose `gateway.task.parse_failures` (Int64Counter) and increment it once per parse-failed model attempt (including retries and escalation attempts), with attributes `reason` (the `ParseError` reason) and `tier`.

### Requirement: Span events
The implement span SHALL record one `file.written` event per written file with attributes `path`, `operation`, `bytes`.

### Requirement: Existing instruments unchanged
All pre-existing counters, histograms, and spans SHALL keep their names, attributes, and emission points.

## Scenarios

### TELEM-1 — files_written per file with attributes
GIVEN a successful run at tier `mid` with complexity `standard` that wrote one create and one modify
WHEN counters are inspected
THEN `gateway.task.files_written` increased by 2 with attribute sets (tier=mid, complexity=standard, operation=create) and (tier=mid, complexity=standard, operation=modify)

### TELEM-2 — write_failures with reason
GIVEN an op rejected as `path_traversal`
WHEN counters are inspected
THEN `gateway.task.write_failures` increased by 1 with attribute reason=path_traversal

### TELEM-3 — parse_failures per failed attempt
GIVEN a run with one parse failure at tier `small` before a successful retry
WHEN counters are inspected
THEN `gateway.task.parse_failures` increased by 1 with attributes reason=<ParseError reason> and tier=small

### TELEM-4 — Span events per file
GIVEN a successful run that wrote two files
WHEN the implement span is inspected
THEN it contains two `file.written` events with attributes path, operation, and bytes

### TELEM-5 — No writes, no counter movement
GIVEN a run that exhausted parse attempts
WHEN counters are inspected
THEN `gateway.task.files_written` is unchanged and `gateway.task.parse_failures` increased by the number of failed attempts
