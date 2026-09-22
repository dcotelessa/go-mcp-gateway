// Package filewriter parses model output into file operations and applies
// them atomically to a task worktree.
//
// The output contract is full file replacement (RD-1): a model emits fenced
// blocks whose info string names a path, and the block body is the complete
// new content of that file. The parser never merges fragments and the writer
// never patches.
//
// Recovery on unparseable output follows a fixed ladder (RD-2): retry once on
// the same tier with a stricter prompt, then escalate one tier. Write failures
// are terminal and never re-prompted.
//
// The parser is pure: it never touches the filesystem. Whether a write
// creates or replaces a file is decided by the writer, which already owns
// path resolution against the worktree.
package filewriter

import "fmt"

// OpKind identifies what a file operation does to its target.
type OpKind string

const (
	// OpWrite is a parsed file: block before classification. The writer
	// resolves it to OpCreate or OpModify against the worktree.
	OpWrite OpKind = "write"

	// OpCreate writes a file that did not exist in the worktree.
	OpCreate OpKind = "create"

	// OpModify replaced the entire contents of an existing file.
	OpModify OpKind = "modify"

	// OpDelete removes a file. Deleting a file that does not exist is not
	// an error; the resulting Change is marked Skipped.
	OpDelete OpKind = "delete"
)

// FileOp is one parsed operation from a model response.
type FileOp struct {
	// Kind is OpWrite for file: blocks and OpDelete for file-delete:
	// blocks. The parser never produces OpCreate or OpModify.
	Kind OpKind

	// RawPath is the path exactly as the model emitted it, before cleaning.
	// Retained for error messages.
	RawPath string

	// Path is the cleaned, worktree-relative path.
	Path string

	// Content is the complete new file content. May be empty: an empty
	// file is a legitimate file. Always empty for OpDelete.
	Content string
}

// Change records one applied operation.
type Change struct {
	// Path is worktree-relative.
	Path string

	// Operation is OpCreate, OpModify, or OpDelete — never OpWrite.
	Operation OpKind

	// Bytes written. Zero for OpDelete.
	Bytes int

	// Skipped is true when the operation had no effect: a delete whose
	// target was already absent. Skipped changes are not reported in
	// files_changed and produce no diff section, but they are counted in
	// telemetry — a model deleting a file that never existed may have
	// hallucinated the codebase.
	Skipped bool
}

// Parse failure reasons. These are machine-readable and drive both the
// retry ladder and the gateway.task.parse_failures metric.
const (
	// ReasonNoOperations means the output contained no file: or
	// file-delete: fences at all.
	ReasonNoOperations = "no_operations"

	// ReasonUnclosedFence means an operation block was opened but never
	// closed before the end of the output.
	ReasonUnclosedFence = "unclosed_fence"

	// ReasonMissingPath means a fence info string carried no path after
	// the file: or file-delete: prefix.
	ReasonMissingPath = "missing_path"

	// ReasonDuplicatePath means two operations resolved to the same
	// worktree-relative path.
	ReasonDuplicatePath = "duplicate_path"
)

// ParseError is returned for any model output that yields no usable set of
// operations. Parse never returns zero operations with a nil error.
type ParseError struct {
	// Reason is one of the Reason* constants above.
	Reason string

	// Detail is a human-readable explanation.
	Detail string

	// Line is a best-effort 1-based line number of the offending fence.
	// Zero when no specific line applies.
	Line int
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("filewriter: parse %s at line %d: %s",
			e.Reason, e.Line, e.Detail)
	}
	return fmt.Sprintf("filewriter: parse %s: %s", e.Reason, e.Detail)
}

// Write failure reasons. These drive the gateway.task.write_failures metric.
// Path-safety rejections use the first three.
const (
	// ReasonPathTraversal means the path resolved outside the worktree,
	// or resolution was ambiguous enough that it could not be ruled out.
	ReasonPathTraversal = "path_traversal"

	// ReasonGitignored means the path matches the worktree's .gitignore.
	ReasonGitignored = "gitignored"

	// ReasonBinaryExtension means the path has a configured binary extension.
	ReasonBinaryExtension = "binary_extension"

	// ReasonNotRegularFile means the target exists but is a directory or
	// another non-regular file.
	ReasonNotRegularFile = "not_regular_file"

	// ReasonDuplicateTarget means two operations in one batch resolved to
	// the same file. The parser cannot catch this: distinct paths can
	// resolve to one file through a symlink inside the worktree.
	ReasonDuplicateTarget = "duplicate_target"

	// ReasonIOError means a filesystem operation failed.
	ReasonIOError = "io_error"
)

// WriteError is returned when an operation is rejected or fails to apply.
// Write failures are terminal: the executor never re-prompts on them.
type WriteError struct {
	// Reason is one of the write reason constants above.
	Reason string

	// Path is the offending path as supplied, worktree-relative where known.
	Path string

	// Detail explains the rejection without disclosing file contents.
	Detail string

	// Err is the underlying cause, if any.
	Err error
}

func (e *WriteError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("filewriter: write %s for %q: %s", e.Reason, e.Path, e.Detail)
	}
	return fmt.Sprintf("filewriter: write %s for %q", e.Reason, e.Path)
}

func (e *WriteError) Unwrap() error { return e.Err }
