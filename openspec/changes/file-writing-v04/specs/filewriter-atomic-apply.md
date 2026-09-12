# Spec: filewriter — Atomic Batch Apply

Covers: applying parsed operations to the worktree with all-or-nothing semantics (`internal/filewriter` writer).

## Requirements

### Requirement: Atomic batch semantics
All operations in one response SHALL succeed or none SHALL be applied. On any failure the worktree SHALL be restored byte-identical to its pre-batch state and no temporary files SHALL remain.

### Requirement: Validation before mutation
Every path in the batch SHALL be validated (see specs/filewriter-path-safety.md) before the first byte is written. Any validation failure SHALL abort the batch with reason `path_traversal`, `gitignored`, or `binary_extension` and perform zero mutations.

### Requirement: Parent directory creation
Create and modify operations SHALL create missing parent directories beneath the worktree.

### Requirement: Delete ordering
Delete operations SHALL be deferred until all create/modify operations in the same batch have been applied. If any create/modify fails, deletes SHALL NOT run.

### Requirement: Failure reasons
The writer SHALL report failures with machine-readable reasons: `not_regular_file` (target path is an existing directory), `delete_missing` (delete target absent), `io_error` (filesystem error). Safety rejections keep their path-safety reasons.

### Requirement: Per-op results
A successful apply SHALL return one `Change` per operation with worktree-relative `Path`, `Operation` kind, and written `Bytes`.

### Requirement: Dry run
`DryRun` SHALL run all validation and return the planned `Change` list without mutating the filesystem. (Library API only in v0.4 — not on the REST/MCP contract.)

### Requirement: Serial execution assumption (non-goal)
The writer MAY assume serial execution per worktree. Concurrent batches against one worktree are out of scope for v0.4 (named in proposal as LOW risk, deferred).

## Scenarios

### APPLY-1 — Batch applies fully
GIVEN a batch of three valid ops (create, modify, delete) in a prepared worktree
WHEN Apply is called
THEN every create/modify target on disk matches the op content exactly, the delete target is gone, and three Changes with path/operation/bytes are returned

### APPLY-2 — Failure rolls back to byte-identical state
GIVEN a batch where the second op targets a path that is an existing directory
WHEN Apply is called
THEN an error with reason `not_regular_file` is returned, the first file is restored to its original content, the third file is absent, and no `.gw-tmp-*` files remain

### APPLY-3 — Parent directories created
GIVEN a create op targeting `internal/graft/new/deep/file.go` where `new/` does not exist
WHEN Apply is called
THEN all missing parent directories are created and the file is written

### APPLY-4 — Deletes deferred until creates/modifies succeed
GIVEN a batch containing a create op that will fail (target is a directory) and a delete op for an existing file
WHEN Apply is called
THEN the file marked for deletion still exists after the failed batch

### APPLY-5 — Delete of missing file
GIVEN a delete op whose target does not exist
WHEN Apply is called
THEN an error with reason `delete_missing` is returned and the rest of the batch is rolled back

### APPLY-6 — Dry run mutates nothing
GIVEN a valid batch
WHEN DryRun is called
THEN the planned Changes are returned, all validations run, and the worktree is byte-identical before and after
