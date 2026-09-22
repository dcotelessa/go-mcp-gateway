# Spec: filewriter — Path Safety

Covers: confining all writes to the task `worktreePath` (`internal/filewriter` path validator).
This is the HIGH-risk control from the proposal: rejection MUST occur before any filesystem write.

## Requirements

### Requirement: Confine all writes to the worktree
Every operation path SHALL be resolved to an absolute path — lexically cleaned, with symlinks evaluated as far as the path exists — and verified to lie inside the worktree BEFORE any write is attempted. The worktree root itself SHALL be canonicalized at construction so containment is compared against a resolved path.

### Requirement: Resolve paths that do not yet exist
Most targets do not exist, since every create writes a new file. The validator SHALL canonicalize the deepest existing ancestor, verify that it lies inside the worktree, then rejoin the remaining components and verify containment again.

### Requirement: Reject traversal and outside-absolute paths
The validator SHALL reject with reason `path_traversal` any path that resolves outside the worktree, whether by `..` traversal, by an absolute path pointing elsewhere, or by a symlink.

### Requirement: Accept absolute paths inside the worktree
An absolute path that resolves inside the worktree SHALL be accepted and normalized to its worktree-relative form.

### Requirement: Refuse ignored and binary targets
The validator SHALL reject paths matching the worktree `.gitignore` (reason `gitignored`) and paths whose extension is refused (reason `binary_extension`).

### Requirement: Binary extensions extend, never replace
The default refused list is `.png .jpg .jpeg .gif .ico .pdf .zip .tar .gz .exe .dll .so .dylib .bin .woff .woff2 .ttf`. Configured extensions SHALL be added to this list. There SHALL be no configuration that removes a default: a configuration mistake must never widen what the gateway is willing to write.

### Requirement: Gitignore matching is a documented subset
The matcher SHALL support comments, blank lines, negation with last-match-wins, anchored patterns, directory-only patterns, `*`, `?`, and `**`. Nested `.gitignore` files in subdirectories are NOT supported. This is a convenience filter, not a security control — containment is what keeps writes inside the worktree — and the limitation SHALL be documented at the implementation.

### Requirement: Invalid root
Constructing a validator with a `worktreePath` that is empty, does not exist, is not a directory, or cannot be canonicalized SHALL fail at construction.

### Requirement: Fail closed
Resolution ambiguity SHALL be a rejection, never an allow. This includes an ancestor that cannot be inspected or resolved, a dangling symlink, and an unreadable `.gitignore` — the last failing construction rather than validating without ignore rules.

## Scenarios

### SAFE-1 — Relative traversal rejected before disk
GIVEN an op with path `../escaped.txt` and a valid worktree
WHEN the path is validated
THEN validation fails with reason `path_traversal` and nothing exists at the attempted target outside the worktree

### SAFE-2 — Absolute path outside worktree rejected
GIVEN an op with absolute path `/etc/hosts`
WHEN the path is validated
THEN validation fails with reason `path_traversal`

### SAFE-3 — Traversal through a valid prefix rejected
GIVEN an op with path `internal/../../escaped.go`
WHEN the path is validated
THEN validation fails with reason `path_traversal` and nothing is created outside the worktree

### SAFE-4 — Absolute path inside worktree accepted
GIVEN an op with an absolute path under the worktree
WHEN the path is validated
THEN it is accepted and normalized to its relative form

### SAFE-5 — Gitignored path rejected
GIVEN a worktree whose `.gitignore` contains `dist/` and `*.log`
WHEN ops target `dist/bundle.js` and `internal/debug.log`
THEN both fail with reason `gitignored`, while an unignored path is accepted

### SAFE-6 — Binary extension rejected
GIVEN ops targeting `assets/logo.png`, `assets/LOGO.PNG`, and `dist/app.so`
WHEN the paths are validated
THEN each fails with reason `binary_extension`

### SAFE-7 — Symlink escape rejected
GIVEN a symlink `<worktree>/out -> /etc`
WHEN an op targets `out/hosts`
THEN validation fails with reason `path_traversal`

### SAFE-8 — Valid relative path resolves
GIVEN an op targeting `internal/rest/implement.go`
WHEN the path is validated
THEN the resolved absolute path is that path under the worktree and validation passes

### SAFE-9 — Invalid worktree root
GIVEN a `worktreePath` that is empty, nonexistent, or a regular file
WHEN a PathValidator is constructed
THEN construction fails and no validator is returned

### SAFE-10 — Configured extensions extend the defaults
GIVEN a SafetyConfig listing `.wasm` and `.WEBP`
WHEN `build/app.wasm`, `assets/hero.webp`, and `assets/logo.png` are validated
THEN all three fail with reason `binary_extension`

### SAFE-11 — Empty config keeps every default
GIVEN a SafetyConfig with no extensions listed
WHEN a path with each default extension is validated
THEN each fails with reason `binary_extension`

### SAFE-12 — Gitignore negation re-includes
GIVEN a `.gitignore` containing `*.env` then `!keep.env`
WHEN `secrets.env` and `keep.env` are validated
THEN the first is rejected and the second is accepted

### SAFE-13 — Internal symlink accepted
GIVEN a symlink `<worktree>/link -> <worktree>/real`
WHEN an op targets `link/a.go`
THEN validation passes and the absolute path is the resolved location under `real/`

### SAFE-14 — Dangling symlink rejected
GIVEN a symlink whose target does not exist
WHEN an op targets a path beneath it
THEN validation fails with reason `path_traversal`

### SAFE-15 — Empty and dot paths rejected
GIVEN paths `""`, `"   "`, `"."`, and `"./"`
WHEN validated
THEN each is rejected
