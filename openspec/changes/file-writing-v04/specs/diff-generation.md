# Spec: Unified Diff Generation

Covers: producing the diff returned to callers and forwarded to the Mastra `/interpret` call (`internal/filewriter` diff builder).

## Requirements

### Requirement: Scope — touched files only
The diff SHALL cover exactly the files touched by the applied batch, in batch order. It MUST NOT be a whole-worktree diff.

### Requirement: Git-style headers
Each file section SHALL use `--- a/<rel>` and `+++ b/<rel>` headers. Created files SHALL use `--- /dev/null`; deleted files SHALL use `+++ /dev/null`.

### Requirement: Unified format with configurable context
Sections SHALL be unified diffs with a configurable number of context lines (default 3, set via `internal/config`).

### Requirement: Empty when nothing written
When no files were written (parse exhaustion, write failure, zero-op edge), the diff SHALL be the empty string.

### Requirement: No git subprocess
Diff generation MUST be in-process. The gateway MUST NOT invoke `git` and MUST NOT touch the git index (staging/commit remains the Mastra workflow's responsibility).

### Requirement: Determinism (SHOULD)
Headers SHOULD contain no timestamps so identical inputs yield byte-identical diffs across runs.

## Scenarios

### DIFF-1 — Modify produces unified hunk
GIVEN a modify op that changed 2 lines of an existing file
WHEN the batch diff is built
THEN the diff contains one file section with `--- a/<rel>` and `+++ b/<rel>` headers and hunks with 3 context lines showing the changed lines

### DIFF-2 — Create shown as all additions
GIVEN a create op
WHEN the diff is built
THEN the section headers are `--- /dev/null` and `+++ b/<rel>` and every content line is an addition

### DIFF-3 — Delete shown as all deletions
GIVEN a delete op
WHEN the diff is built
THEN the section headers are `--- a/<rel>` and `+++ /dev/null` and every original line is a deletion

### DIFF-4 — No writes yields empty diff
GIVEN a run that wrote no files
WHEN the response is assembled
THEN Diff is ""

### DIFF-5 — No-op modify has no hunk
GIVEN a modify op whose content is byte-identical to the existing file
WHEN the diff is built
THEN the file appears in Changes but contributes no hunks to the diff
