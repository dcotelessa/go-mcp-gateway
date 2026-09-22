# Spec: filewriter — Atomic Batch Apply

Covers: applying parsed operations to the worktree with all-or-nothing semantics (`internal/filewriter` writer).
Amended by **AM-2** (delete is idempotent) and **AM-3** (the writer classifies create vs modify). See design.md § Amendments.

## Requirements

### Requirement: Atomic batch semantics
All operations in one response SHALL succeed or none SHALL be applied. On any failure the worktree SHALL be restored byte-identical to its pre-batch state and no temporary files SHALL remain.

### Requirement: Validation before mutation
Every path in the batch SHALL be validated (see filewriter-path-safety.md) before the first byte is written. Any validation failure SHALL abort the batch with reason `path_traversal`, `gitignored`, or `binary_extension` and perform zero mutations.

### Requirement: Classification by the writer
The writer SHALL classify each `OpWrite` during validation, using the validator-resolved absolute path: `OpModify` when that path is an existing regular file, `OpCreate` when it does not exist. Classification and path resolution SHALL happen in one place so they cannot disagree.

### Requirement: Parent directory creation
Create operations SHALL create missing parent directories beneath the worktree.

### Requirement: Delete is idempotent
A delete whose target does not exist SHALL succeed and produce a `Change` with `Operation` delete and `Skipped` true. It MUST NOT fail or roll back the batch. The executor SHALL count skipped deletes in telemetry, since a model deleting a file that never existed may have hallucinated the codebase.

### Requirement: Delete ordering
Delete operations SHALL be deferred until all create/modify operations in the same batch have been applied. If any create/modify fails, deletes SHALL NOT run.

### Requirement: One target per batch
Two operations whose paths resolve to the same file SHALL be rejected with reason `duplicate_target`. The parser cannot detect this: distinct paths can resolve to one file through a symlink inside the worktree, and silently letting the later write win would discard a file the model intended to produce.

### Requirement: Preserve file mode
A modify SHALL preserve the target's existing permission bits. A create SHALL use 0644.

### Requirement: Cancellation
A cancelled context SHALL stop the batch and roll back, reported as `io_error`.

### Requirement: Failure reasons
The writer SHALL report failures with machine-readable reasons: `not_regular_file` (target exists and is not a regular file), `duplicate_target` (two operations resolve to one file), `io_error` (filesystem error, including cancellation). Safety rejections keep their path-safety reasons.

### Requirement: Per-op results
A successful apply SHALL return one `Change` per operation with worktree-relative `Path`, classified `Operation` (never `write`), written `Bytes`, and `Skipped`.

### Requirement: Dry run
`DryRun` SHALL run all validation and classification and return the planned `Change` list without mutating the filesystem. (Library API only in v0.4 — not on the REST/MCP contract.)

### Requirement: Serial execution assumption (non-goal)
The writer MAY assume serial execution per worktree. Concurrent batches against one worktree are out of scope for v0.4.

## Scenarios

### APPLY-1 — Batch applies fully
GIVEN a batch of three valid ops (a write to a new path, a write to an existing path, a delete of an existing file) in a prepared worktree
WHEN Apply is called
THEN every written target on disk matches the op content exactly, the delete target is gone, and three Changes are returned with operations create, modify, delete

### APPLY-2 — Failure rolls back to byte-identical state
GIVEN a batch where the second op targets a path that is an existing directory
WHEN Apply is called
THEN an error with reason `not_regular_file` is returned, the first file is restored to its original content, the third file is absent, and no `.gw-tmp-*` files remain

### APPLY-3 — Parent directories created
GIVEN a write targeting `internal/graft/new/deep/file.go` where `new/` does not exist
WHEN Apply is called
THEN all missing parent directories are created and the file is written

### APPLY-4 — Deletes deferred until writes succeed
GIVEN a batch containing a write that will fail (target is a directory) and a delete op for an existing file
WHEN Apply is called
THEN the file marked for deletion still exists after the failed batch

### APPLY-5 — Delete of a missing file is skipped, not failed
GIVEN a batch with one valid write and one delete whose target does not exist
WHEN Apply is called
THEN the call succeeds, the write is applied, and the delete's Change has Skipped true

### APPLY-6 — Dry run mutates nothing
GIVEN a valid batch
WHEN DryRun is called
THEN the planned Changes are returned with classified operations, all validations run, and the worktree is byte-identical before and after

### APPLY-7 — Writer classifies create vs modify
GIVEN one write to an existing regular file and one write to an absent path
WHEN Apply or DryRun is called
THEN the Changes report `modify` and `create` respectively

### APPLY-8 — Empty file write
GIVEN a write with empty Content to an absent path
WHEN Apply is called
THEN a zero-byte file exists at the path and the Change reports create with Bytes 0

### APPLY-9 — Two paths resolving to one file
GIVEN a symlink inside the worktree and two writes, one through it and one direct, that resolve to the same file
WHEN Apply or DryRun is called
THEN the batch is rejected with reason `duplicate_target` and nothing is written

### APPLY-10 — Modify preserves permission bits
GIVEN an existing file with mode 0755
WHEN a modify is applied
THEN the file keeps mode 0755

### APPLY-11 — Cancelled context rolls back
GIVEN a cancelled context
WHEN Apply is called
THEN an error is returned, the worktree is byte-identical, and no staging files remain
