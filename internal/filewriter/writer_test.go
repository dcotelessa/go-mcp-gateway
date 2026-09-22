package filewriter

import (
	"os"
	"path/filepath"
	"testing"
)

func newWriter(t *testing.T, root string) *Writer {
	t.Helper()
	return NewWriter(newValidator(t, root))
}

// snapshotTree records every regular file under root with its content, so a
// test can assert the worktree is byte-identical after a failure.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}

func assertTreeEqual(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("tree size changed: want %d files, got %d\nwant=%v\ngot=%v",
			len(want), len(got), keys(want), keys(got))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: content changed\nwant %q\ngot  %q", k, v, got[k])
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// assertNoTempFiles fails if any staging file survived.
func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if matched, _ := filepath.Match(tempPattern, filepath.Base(path)); matched {
			t.Errorf("staging file left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// APPLY-6 — Dry run mutates nothing.
func TestDryRun_ReturnsPlanWithoutMutation(t *testing.T) {
	root := newWorktree(t, map[string]string{
		"internal/a.go": "package a\n",
		"internal/b.go": "package b\n",
	})
	before := snapshotTree(t, root)

	changes, err := newWriter(t, root).DryRun([]FileOp{
		{Kind: OpWrite, Path: "internal/a.go", Content: "package a\n\nvar X = 1\n"},
		{Kind: OpWrite, Path: "internal/new.go", Content: "package internal\n"},
		{Kind: OpDelete, Path: "internal/b.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}
	assertTreeEqual(t, before, snapshotTree(t, root))
}

// APPLY-7 — Writer classifies create vs modify.
func TestWriter_ClassifiesCreateVsModify(t *testing.T) {
	root := newWorktree(t, map[string]string{"internal/existing.go": "package existing\n"})

	changes, err := newWriter(t, root).DryRun([]FileOp{
		{Kind: OpWrite, Path: "internal/existing.go", Content: "package existing\n\nvar V = 1\n"},
		{Kind: OpWrite, Path: "internal/absent.go", Content: "package absent\n"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changes[0].Operation != OpModify {
		t.Errorf("existing target: got %q, want %q", changes[0].Operation, OpModify)
	}
	if changes[1].Operation != OpCreate {
		t.Errorf("absent target: got %q, want %q", changes[1].Operation, OpCreate)
	}
}

func TestDryRun_RejectsUnsafePathBeforeAnything(t *testing.T) {
	root := newWorktree(t, nil)

	_, err := newWriter(t, root).DryRun([]FileOp{
		{Kind: OpWrite, Path: "../escaped.go", Content: "x"},
	})
	assertRejected(t, err, ReasonPathTraversal,
		filepath.Join(filepath.Dir(root), "escaped.go"))
}

// A directory target is refused before any mutation.
func TestPlan_RejectsDirectoryTarget(t *testing.T) {
	root := newWorktree(t, map[string]string{"internal/pkg/a.go": "package pkg\n"})

	_, err := newWriter(t, root).DryRun([]FileOp{
		{Kind: OpWrite, Path: "internal/pkg", Content: "not a directory"},
	})
	assertRejected(t, err, ReasonNotRegularFile)
}

// APPLY-9 — Two paths resolving to one file are refused.
func TestPlan_RejectsDuplicateResolvedTarget(t *testing.T) {
	root := newWorktree(t, map[string]string{"real/a.go": "package a\n"})
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := newWriter(t, root).DryRun([]FileOp{
		{Kind: OpWrite, Path: "real/a.go", Content: "package a\n// one\n"},
		{Kind: OpWrite, Path: "link/a.go", Content: "package a\n// two\n"},
	})
	assertRejected(t, err, ReasonDuplicateTarget)
}
