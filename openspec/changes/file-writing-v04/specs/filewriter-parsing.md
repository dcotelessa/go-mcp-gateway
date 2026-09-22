# Spec: filewriter — Structured Output Parsing

Covers: parsing model output into file operations (`internal/filewriter` parser).
Decisions applied: **RD-1** — full file replacement; not unified diff patches, not search/replace blocks.
Amended by **AM-1** (empty files are legitimate) and **AM-3** (parser is pure; classification moves to the writer). See design.md § Amendments.

## Requirements

### Requirement: Fence contract is the only operation source
The parser SHALL treat ONLY fenced blocks whose info string starts with `file:` (write) or `file-delete:` (delete) as file operations. Fences with any other info string (e.g. a plain ```go block) MUST be ignored entirely, including any backtick sequences inside them.

### Requirement: Full replacement content
Per RD-1, a `file:` block body SHALL be treated as the complete new content of the target file. The parser MUST NOT merge, patch, or diff bodies against existing content, and MUST NOT support hunk or search/replace syntax.

### Requirement: Parser is pure
The parser SHALL NOT access the filesystem. It SHALL emit `file:` blocks as `OpWrite` and `file-delete:` blocks as `OpDelete`. It SHALL NOT produce `OpCreate` or `OpModify`; classifying a write as create or modify is the writer's responsibility (see filewriter-atomic-apply.md).

### Requirement: Ordered operations
The parser SHALL return one `FileOp` per operation block, in the order the blocks appear in the model output.

### Requirement: Empty bodies are empty files
A `file:` block with an empty body SHALL parse as an `OpWrite` with empty `Content`. An empty file is a legitimate file (package markers, placeholder fixtures) and MUST NOT be treated as an error.

### Requirement: Structured parse failure
The parser SHALL return `*ParseError` with a machine-readable `Reason` and human-readable `Detail` for every unparseable input. It MUST NOT return zero operations with a nil error. Reasons SHALL be exactly: `no_operations`, `unclosed_fence`, `missing_path`, `duplicate_path`.

### Requirement: Content fidelity
`FileOp.Content` SHALL contain exactly the body between the opening fence line and the closing fence, excluding the single newline immediately preceding the closing fence. No prose, fence markers, or info strings SHALL appear in `Content`.

## Scenarios

### PARSE-1 — Multiple blocks parse in order
GIVEN model output containing prose, two `file:` blocks, and a plain ```go fence
WHEN the output is parsed
THEN exactly two ops are returned in block order with the correct paths and exact bodies, and no prose or fence markers appear in any Content

### PARSE-2 — File blocks are unclassified writes
GIVEN a `file:` block, whether or not its target exists on disk
WHEN the output is parsed
THEN the op kind is `write`, and no filesystem access occurs

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
THEN a `*ParseError` with Reason `unclosed_fence` is returned, with Line set to the opening fence

### PARSE-6 — Missing path
GIVEN a `file:` block whose info string contains no path
WHEN the output is parsed
THEN a `*ParseError` with Reason `missing_path` is returned

### PARSE-7 — Empty body is an empty file
GIVEN a `file:` block with an empty body
WHEN the output is parsed
THEN one `write` op with empty Content is returned and no error

### PARSE-8 — Non-file fences ignored
GIVEN output containing a plain ```go fence and one `file:` fence
WHEN the output is parsed
THEN only the `file:` fence yields an op

### PARSE-9 — Duplicate target
GIVEN two `file:` blocks whose paths clean to the same worktree-relative path
WHEN the output is parsed
THEN a `*ParseError` with Reason `duplicate_path` is returned

### PARSE-10 — ParseError message quality
GIVEN a `*ParseError` with any Reason and Detail
WHEN `Error()` is called
THEN the message contains both the Reason token and the Detail text

### PARSE-11 — Backticks inside a non-operation fence
GIVEN a ```markdown fence whose body contains an indented line beginning with a `file:` fence
WHEN the output is parsed
THEN that inner line does not produce an op
