package filewriter

import (
	"errors"
	"strings"
	"testing"
)

// parseErr extracts a *ParseError or fails the test.
func parseErr(t *testing.T, err error) *ParseError {
	t.Helper()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	return pe
}

// PARSE-1 — Multiple blocks parse in order.
func TestParse_MultipleBlocksInOrder(t *testing.T) {
	output := "Here is the plan.\n" +
		"\n" +
		"```file:internal/a.go\n" +
		"package a\n" +
		"\n" +
		"const X = 1\n" +
		"```\n" +
		"\n" +
		"Some prose between blocks.\n" +
		"\n" +
		"```go\n" +
		"this is not an operation\n" +
		"```\n" +
		"\n" +
		"```file:internal/b.go\n" +
		"package b\n" +
		"```\n" +
		"\n" +
		"Trailing prose.\n"

	ops, err := NewParser().Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d: %+v", len(ops), ops)
	}
	if ops[0].Path != "internal/a.go" || ops[1].Path != "internal/b.go" {
		t.Errorf("wrong order or paths: %q, %q", ops[0].Path, ops[1].Path)
	}
	if want := "package a\n\nconst X = 1"; ops[0].Content != want {
		t.Errorf("op[0] content = %q, want %q", ops[0].Content, want)
	}
	if ops[1].Content != "package b" {
		t.Errorf("op[1] content = %q", ops[1].Content)
	}
	for i, op := range ops {
		if strings.Contains(op.Content, fenceMarker) {
			t.Errorf("op[%d] content contains a fence marker: %q", i, op.Content)
		}
	}
}

// PARSE-2 — File blocks parse as unclassified writes.
// The parser never touches the filesystem; create vs modify is the writer's
// decision (APPLY-7).
func TestParse_FileBlocksAreUnclassifiedWrites(t *testing.T) {
	ops, err := NewParser().Parse("```file:internal/existing.go\npackage existing\n```\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ops[0].Kind != OpWrite {
		t.Errorf("kind = %q, want %q", ops[0].Kind, OpWrite)
	}
}

// PARSE-3 — Delete blocks.
func TestParse_DeleteBlock(t *testing.T) {
	ops, err := NewParser().Parse("```file-delete:old.go\n```\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != OpDelete {
		t.Errorf("kind = %q, want %q", ops[0].Kind, OpDelete)
	}
	if ops[0].Path != "old.go" {
		t.Errorf("path = %q", ops[0].Path)
	}
	if ops[0].Content != "" {
		t.Errorf("delete content should be empty, got %q", ops[0].Content)
	}
}

// PARSE-4 — No operations.
func TestParse_NoOperations(t *testing.T) {
	_, err := NewParser().Parse("I would restructure the swap queue as follows.\n\n```go\nfunc main() {}\n```\n")
	if pe := parseErr(t, err); pe.Reason != ReasonNoOperations {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonNoOperations)
	}
}

// PARSE-5 — Unclosed fence.
func TestParse_UnclosedFence(t *testing.T) {
	_, err := NewParser().Parse("```file:internal/a.go\npackage a\n\nconst X = 1\n")
	pe := parseErr(t, err)
	if pe.Reason != ReasonUnclosedFence {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonUnclosedFence)
	}
	if pe.Line != 1 {
		t.Errorf("line = %d, want 1", pe.Line)
	}
}

// PARSE-6 — Missing path.
func TestParse_MissingPath(t *testing.T) {
	_, err := NewParser().Parse("```file:\npackage a\n```\n")
	if pe := parseErr(t, err); pe.Reason != ReasonMissingPath {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonMissingPath)
	}
}

// PARSE-7 — Empty body is an empty file, not an error.
func TestParse_EmptyBodyIsEmptyFile(t *testing.T) {
	ops, err := NewParser().Parse("```file:pkg/__init__.py\n```\n")
	if err != nil {
		t.Fatalf("empty body should parse, got: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != OpWrite || ops[0].Content != "" {
		t.Errorf("got kind=%q content=%q, want write with empty content",
			ops[0].Kind, ops[0].Content)
	}
}

// PARSE-8 — Non-file fences ignored.
func TestParse_IgnoresNonFileFences(t *testing.T) {
	output := "```go\nnot an operation\n```\n" +
		"```file:internal/a.go\npackage a\n```\n" +
		"```bash\nalso not an operation\n```\n"

	ops, err := NewParser().Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d: %+v", len(ops), ops)
	}
	if ops[0].Path != "internal/a.go" {
		t.Errorf("path = %q", ops[0].Path)
	}
}

// PARSE-9 — Duplicate target.
func TestParse_DuplicatePath(t *testing.T) {
	output := "```file:internal/a.go\npackage a\n```\n" +
		"```file:./internal/a.go\npackage a2\n```\n"

	_, err := NewParser().Parse(output)
	if pe := parseErr(t, err); pe.Reason != ReasonDuplicatePath {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonDuplicatePath)
	}
}

// Backticks inside a non-operation fence must not open an operation.
func TestParse_BackticksInsideNonOpFenceIgnored(t *testing.T) {
	output := "```markdown\n" +
		"Example:\n" +
		"    ```file:should/not/parse.go\n" +
		"```\n" +
		"```file:real.go\npackage real\n```\n"

	ops, err := NewParser().Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 || ops[0].Path != "real.go" {
		t.Errorf("expected only real.go, got %+v", ops)
	}
}
