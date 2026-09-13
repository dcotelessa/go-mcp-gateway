package filewriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeWith builds a temp worktree containing the given files.
func worktreeWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return root
}

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
	root := worktreeWith(t, nil)
	p := NewParser(root)

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

	ops, err := p.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d: %+v", len(ops), ops)
	}
	if ops[0].Path != "internal/a.go" || ops[1].Path != "internal/b.go" {
		t.Errorf("wrong order or paths: %q, %q", ops[0].Path, ops[1].Path)
	}
	wantA := "package a\n\nconst X = 1"
	if ops[0].Content != wantA {
		t.Errorf("op[0] content = %q, want %q", ops[0].Content, wantA)
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

// PARSE-2 — Kind classification by existence.
func TestParse_KindClassification(t *testing.T) {
	root := worktreeWith(t, map[string]string{
		"internal/existing.go": "package existing\n",
	})
	p := NewParser(root)

	ops, err := p.Parse("```file:internal/existing.go\npackage existing\n\nvar V = 1\n```\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ops[0].Kind != OpModify {
		t.Errorf("existing file: got %q, want %q", ops[0].Kind, OpModify)
	}

	ops, err = p.Parse("```file:internal/brand-new.go\npackage brandnew\n```\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ops[0].Kind != OpCreate {
		t.Errorf("absent file: got %q, want %q", ops[0].Kind, OpCreate)
	}
}

// PARSE-3 — Delete blocks.
func TestParse_DeleteBlock(t *testing.T) {
	root := worktreeWith(t, map[string]string{"old.go": "package old\n"})
	p := NewParser(root)

	ops, err := p.Parse("```file-delete:old.go\n```\n")
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
	p := NewParser(t.TempDir())

	_, err := p.Parse("I would restructure the swap queue as follows.\n\n```go\nfunc main() {}\n```\n")
	pe := parseErr(t, err)
	if pe.Reason != ReasonNoOperations {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonNoOperations)
	}
}

// PARSE-5 — Unclosed fence.
func TestParse_UnclosedFence(t *testing.T) {
	p := NewParser(t.TempDir())

	_, err := p.Parse("```file:internal/a.go\npackage a\n\nconst X = 1\n")
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
	p := NewParser(t.TempDir())

	_, err := p.Parse("```file:\npackage a\n```\n")
	pe := parseErr(t, err)
	if pe.Reason != ReasonMissingPath {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonMissingPath)
	}
}

// PARSE-7 — Empty content.
func TestParse_EmptyContent(t *testing.T) {
	p := NewParser(t.TempDir())

	_, err := p.Parse("```file:internal/a.go\n```\n")
	pe := parseErr(t, err)
	if pe.Reason != ReasonEmptyContent {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonEmptyContent)
	}
}

// PARSE-8 — Non-file fences ignored.
func TestParse_IgnoresNonFileFences(t *testing.T) {
	p := NewParser(t.TempDir())

	output := "```go\nnot an operation\n```\n" +
		"```file:internal/a.go\npackage a\n```\n" +
		"```bash\nalso not an operation\n```\n"

	ops, err := p.Parse(output)
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
	p := NewParser(t.TempDir())

	output := "```file:internal/a.go\npackage a\n```\n" +
		"```file:./internal/a.go\npackage a2\n```\n"

	_, err := p.Parse(output)
	pe := parseErr(t, err)
	if pe.Reason != ReasonDuplicatePath {
		t.Errorf("reason = %q, want %q", pe.Reason, ReasonDuplicatePath)
	}
}
