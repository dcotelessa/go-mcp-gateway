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
package filewriter

import "fmt"

// OpKind identifies what a file operation does to its target.
type OpKind string

const (
	// OpCreate writes a file that does not yet exist in the worktree.
	OpCreate OpKind = "create"
	// OpModify replaces the entire contents of an existing file.
	OpModify OpKind = "modify"
	// OpDelete removes an existing file.
	OpDelete OpKind = "delete"
)

// FileOp is one parsed operation from a model response.
type FileOp struct {
	// Kind is create, modify, or delete. Classified at parse time by whether
	// the resolved target already exists in the worktree.
	Kind OpKind

	// RawPath is the path exactly as the model emitted it, before cleaning
	// or resolution. Retained for error messages.
	RawPath string

	// Path is the cleaned, worktree-relative path.
	Path string

	// Content is the complete new file content. Empty for OpDelete.
	Content string
}

// Change records one successfully applied operation.
type Change struct {
	// Path is worktree-relative.
	Path string

	// Operation is the kind that was applied.
	Operation OpKind

	// Bytes written. Zero for OpDelete.
	Bytes int
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

	// ReasonEmptyContent means a file: block had an empty body. Deletes
	// are exempt — they are expected to have no body.
	ReasonEmptyContent = "empty_content"

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
