# Spec: filewriter — Path Safety

Covers: confining all writes to the task `worktreePath` (`internal/filewriter` path validator).
This is the HIGH-risk control from the proposal: rejection MUST occur before any filesystem call.

## Requirements

### Requirement: Confine all writes to the worktree
Every operation path SHALL be resolved (path Clean + symlink evaluation) to an absolute path and verified to lie inside the task's `worktreePath` BEFORE any filesystem read or write is attempted.

### Requirement: Reject traversal and outside-absolute paths
The validator SHALL reject with reason `path_traversal` any path that resolves outside the worktree, whether by `..` traversal or by an absolute path pointing elsewhere on the filesystem.

### Requirement: Accept absolute paths inside the worktree
An absolute path that resolves inside the worktree SHALL be accepted and normalized to its worktree-relative form.

### Requirement: Refuse ignored and binary targets
The validator SHALL reject paths matching the worktree `.gitignore` (reason `gitignored`) and paths whose extension is in the configured binary-extension list (reason `binary_extension`). Default list: `.png .jpg .jpeg .gif .ico .pdf .zip .tar .gz .exe .dll .so .dylib .bin .woff .woff2 .ttf`, overridable via `internal/config`.

### Requirement: Symlink escape
A target whose final component or any ancestor is a symlink resolving outside the worktree SHALL be rejected with reason `path_traversal`.

### Requirement: Invalid root
Constructing a validator with a `worktreePath` that does not exist or is not a directory SHALL fail at construction.

### Requirement: Fail closed (SHOULD)
The validator SHOULD treat resolution ambiguity (unreadable ancestor, dangling symlink) as a rejection, never as an allow.

## Scenarios

### SAFE-1 — Relative traversal rejected before disk
GIVEN an op with path `../../etc/passwd` and a valid worktree
WHEN the path is validated
THEN validation fails with reason `path_traversal` and no file or directory exists at the attempted target outside the worktree

### SAFE-2 — Absolute path outside worktree rejected
GIVEN an op with absolute path `/etc/hosts`
WHEN the path is validated
THEN validation fails with reason `path_traversal`

### SAFE-3 — Traversal through a valid prefix rejected
GIVEN an op with path `internal/../../escaped.go` that resolves outside the worktree
WHEN the path is validated
THEN validation fails with reason `path_traversal`

### SAFE-4 — Absolute path inside worktree accepted
GIVEN an op with absolute path `<worktree>/internal/config/config.go`
WHEN the path is validated
THEN it is accepted and normalized to relative `internal/config/config.go`

### SAFE-5 — Gitignored path rejected
GIVEN a worktree whose `.gitignore` contains `dist/`
WHEN an op targets `dist/bundle.js`
THEN validation fails with reason `gitignored`

### SAFE-6 — Binary extension rejected
GIVEN an op targeting `assets/logo.png`
WHEN the path is validated
THEN validation fails with reason `binary_extension`

### SAFE-7 — Symlink escape rejected
GIVEN a symlink `<worktree>/out -> /etc`
WHEN an op targets `out/hosts`
THEN validation fails with reason `path_traversal`

### SAFE-8 — Valid relative path resolves
GIVEN an op targeting `internal/rest/implement.go`
WHEN the path is validated
THEN the resolved absolute path equals `<worktree>/internal/rest/implement.go` and validation passes

### SAFE-9 — Invalid worktree root
GIVEN a `worktreePath` that does not exist or is a regular file
WHEN a PathValidator is constructed
THEN construction fails with an error and no validator is returned
