# Spec: filewriter — Structured Output Parsing

Covers: parsing model output into file operations (`internal/filewriter` parser).
Decisions applied: **RD-1** — full file replacement; not unified diff patches, not search/replace blocks.

## Requirements

### Requirement: Fence contract is the only operation source
The parser SHALL treat ONLY fenced blocks whose info string starts with `file:` (create/modify) or `file-delete:` (delete) as file operations. Fences with any other info string (e.g. a plain ```go block) MUST be ignored entirely.

### Requirement: Full replacement content
Per RD-1, a `file:` block body SHALL be treated as the complete new content of the target file. The parser MUST NOT merge, patch, or diff bodies against existing content, and MUST NOT support hunk or search/replace syntax.

### Requirement: Ordered operations
The parser SHALL return one `FileOp` per operation block, in the order the blocks appear in the model output.

### Requirement: Kind classification
The parser SHALL classify a `file:` op as `modify` when the target exists in the worktree and `create` when it does not. A `file-delete:` op SHALL always be `delete` with empty `Content`.

### Requirement: Structured parse failure
The parser SHALL return `*ParseError` with a machine-readable `Reason` and human-readable `Detail` for every unparseable input. It MUST NOT return zero operations with a nil error. Reasons SHALL be exactly: `no_operations`, `unclosed_fence`, `missing_path`, `empty_content`, `duplicate_path`.

### Requirement: Content fidelity
`FileOp.Content` SHALL contain exactly the body between the opening fence line and the closing fence, excluding the single newline immediately following the opening fence line. No prose, fence markers, or info strings SHALL appear in `Content`.

## Scenarios

### PARSE-1 — Multiple blocks parse in order
GIVEN model output containing prose, two `file:` blocks, and a plain ```go fence
WHEN the output is parsed
THEN exactly two ops are returned in block order with the correct paths and exact bodies, and no prose or fence markers appear in any Content

### PARSE-2 — Kind classification by existence
GIVEN a `file:` block targeting a path that exists in the worktree
WHEN the output is parsed
THEN the op kind is `modify`
GIVEN a `file:` block targeting a path that does not exist
WHEN the output is parsed
THEN the op kind is `create`

### PARSE-3 — Delete blocks
GIVEN a `file-delete:<path>` block
WHEN the output is parsed
THEN one `delete` op with that path and empty Content is returned

### PARSE-4 — No operations
GIVEN model output containing no `file:` or `file-delete:` fences
WHEN the output is parsed
THEN a `*ParseError` with Reason `no_operations` is returned

### PARSE-5 — Unclosed fence
GIVEN a `file:` block with no closing fence before end of output
WHEN the output is parsed
THEN a `*ParseError` with Reason `unclosed_fence` is returned

### PARSE-6 — Missing path
GIVEN a `file:` block whose info string contains no path
WHEN the output is parsed
THEN a `*ParseError` with Reason `missing_path` is returned

### PARSE-7 — Empty content
GIVEN a `file:` block with an empty body
WHEN the output is parsed
THEN a `*ParseError` with Reason `empty_content` is returned

### PARSE-8 — Non-file fences ignored
GIVEN output containing a plain ```go fence and one `file:` fence
WHEN the output is parsed
THEN only the `file:` fence yields an op

### PARSE-9 — Duplicate target
GIVEN two `file:` blocks resolving to the same worktree-relative path
WHEN the output is parsed
THEN a `*ParseError` with Reason `duplicate_path` is returned

### PARSE-10 — ParseError message quality
GIVEN a `*ParseError` with any Reason and Detail
WHEN `Error()` is called
THEN the message contains both the Reason token and the Detail text
