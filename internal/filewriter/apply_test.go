package filewriter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mustApply(t *testing.T, w *Writer, ops []FileOp) *Batch {
	t.Helper()
	batch, err := w.Apply(context.Background(), ops)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return batch
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// APPLY-1 — Batch applies fully.
func TestApply_BatchSucceedsFully(t *testing.T) {
	root := newWorktree(t, map[string]string{
		"internal/existing.go": "package existing\n",
		"internal/doomed.go":   "package doomed\n",
	})
	w := newWriter(t, root)

	batch := mustApply(t, w, []FileOp{
		{Kind: OpWrite, Path: "internal/brand_new.go", Content: "package brandnew\n"},
		{Kind: OpWrite, Path: "internal/existing.go", Content: "package existing\n\nvar V = 1\n"},
		{Kind: OpDelete, Path: "internal/doomed.go"},
	})

	if got := readFile(t, filepath.Join(root, "internal/brand_new.go")); got != "package brandnew\n" {
		t.Errorf("created content = %q", got)
	}
	if got := readFile(t, filepath.Join(root, "internal/existing.go")); got != "package existing\n\nvar V = 1\n" {
		t.Errorf("modified content = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "internal/doomed.go")); !os.IsNotExist(err) {
		t.Error("deleted file still present")
	}

	want := []OpKind{OpCreate, OpModify, OpDelete}
	if len(batch.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(batch.Changes))
	}
	for i, op := range want {
		if batch.Changes[i].Operation != op {
			t.Errorf("change[%d] = %q, want %q", i, batch.Changes[i].Operation, op)
		}
	}
	if batch.Changes[0].Bytes != len("package brandnew\n") {
		t.Errorf("bytes = %d", batch.Changes[0].Bytes)
	}
	assertNoTempFiles(t, root)
}

// APPLY-2 — Failure rolls back to byte-identical state.
func TestApply_RollbackByteIdentical(t *testing.T) {
	root := newWorktree(t, map[string]string{
		"internal/first.go":     "package first\n",
		"internal/blocked/x.go": "package blocked\n",
		"internal/third.go":     "package third\n",
	})
	w := newWriter(t, root)
	before := snapshotTree(t, root)

	// The second op targets an existing directory.
	_, err := w.Apply(context.Background(), []FileOp{
		{Kind: OpWrite, Path: "internal/first.go", Content: "package first\n\nvar A = 1\n"},
		{Kind: OpWrite, Path: "internal/blocked", Content: "should not be written"},
		{Kind: OpDelete, Path: "internal/third.go"},
	})
	assertRejected(t, err, ReasonNotRegularFile)

	assertTreeEqual(t, before, snapshotTree(t, root))
	assertNoTempFiles(t, root)
}

// Rollback after a mid-batch failure restores files already written.
func TestApply_RollbackRestoresEarlierWrites(t *testing.T) {
	root := newWorktree(t, map[string]string{
		"a.go":    "package a\n",
		"blocked": "",
	})
	// Replace "blocked" with a directory so the second write fails.
	if err := os.Remove(filepath.Join(root, "blocked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := newWriter(t, root)
	before := snapshotTree(t, root)

	_, err := w.Apply(context.Background(), []FileOp{
		{Kind: OpWrite, Path: "a.go", Content: "package a\n\nvar Changed = true\n"},
		{Kind: OpWrite, Path: "new/deep/file.go", Content: "package deep\n"},
		{Kind: OpWrite, Path: "blocked", Content: "x"},
	})
	if err == nil {
		t.Fatal("expected failure")
	}

	assertTreeEqual(t, before, snapshotTree(t, root))
	if _, statErr := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(statErr) {
		t.Error("rollback left the created directory behind")
	}
	assertNoTempFiles(t, root)
}

// APPLY-3 — Parent directories created.
func TestApply_CreatesParentDirs(t *testing.T) {
	root := newWorktree(t, nil)
	w := newWriter(t, root)

	mustApply(t, w, []FileOp{
		{Kind: OpWrite, Path: "internal/graft/new/deep/file.go", Content: "package deep\n"},
	})

	if got := readFile(t, filepath.Join(root, "internal/graft/new/deep/file.go")); got != "package deep\n" {
		t.Errorf("content = %q", got)
	}
}

// APPLY-4 — Deletes deferred until writes succeed.
func TestApply_DeletesDeferredUntilSuccess(t *testing.T) {
	root := newWorktree(t, map[string]string{
		"keep.go": "package keep\n",
		"blocked": "",
	})
	if err := os.Remove(filepath.Join(root, "blocked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := newWriter(t, root)

	_, err := w.Apply(context.Background(), []FileOp{
		{Kind: OpWrite, Path: "blocked", Content: "x"},
		{Kind: OpDelete, Path: "keep.go"},
	})
	if err == nil {
		t.Fatal("expected failure")
	}

	if _, statErr := os.Stat(filepath.Join(root, "keep.go")); statErr != nil {
		t.Errorf("file marked for deletion was removed despite a failed batch: %v", statErr)
	}
	assertNoTempFiles(t, root)
}

// APPLY-5 — Delete of a missing file is skipped, not failed.
func TestApply_DeleteMissingIsSkipped(t *testing.T) {
	root := newWorktree(t, nil)
	w := newWriter(t, root)

	batch := mustApply(t, w, []FileOp{
		{Kind: OpWrite, Path: "present.go", Content: "package present\n"},
		{Kind: OpDelete, Path: "never_existed.go"},
	})

	if got := readFile(t, filepath.Join(root, "present.go")); got != "package present\n" {
		t.Errorf("write content = %q", got)
	}
	if len(batch.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(batch.Changes))
	}
	if !batch.Changes[1].Skipped {
		t.Error("delete of an absent file should be marked Skipped")
	}
	if batch.Changes[1].Operation != OpDelete {
		t.Errorf("operation = %q", batch.Changes[1].Operation)
	}
}

// APPLY-8 — Empty file write.
func TestApply_EmptyFileWrite(t *testing.T) {
	root := newWorktree(t, nil)
	w := newWriter(t, root)

	batch := mustApply(t, w, []FileOp{
		{Kind: OpWrite, Path: "pkg/__init__.py", Content: ""},
	})

	info, err := os.Stat(filepath.Join(root, "pkg/__init__.py"))
	if err != nil {
		t.Fatalf("empty file not created: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
	if batch.Changes[0].Operation != OpCreate || batch.Changes[0].Bytes != 0 {
		t.Errorf("change = %+v", batch.Changes[0])
	}
}

// An existing file keeps its permission bits through a modify.
func TestApply_PreservesFileMode(t *testing.T) {
	root := newWorktree(t, map[string]string{"script.sh": "#!/bin/sh\necho old\n"})
	target := filepath.Join(root, "script.sh")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}

	mustApply(t, newWriter(t, root), []FileOp{
		{Kind: OpWrite, Path: "script.sh", Content: "#!/bin/sh\necho new\n"},
	})

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
}

// A cancelled context stops the batch and rolls back.
func TestApply_ContextCancelledRollsBack(t *testing.T) {
	root := newWorktree(t, map[string]string{"a.go": "package a\n"})
	w := newWriter(t, root)
	before := snapshotTree(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := w.Apply(ctx, []FileOp{
		{Kind: OpWrite, Path: "a.go", Content: "package a\n\nvar X = 1\n"},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}

	assertTreeEqual(t, before, snapshotTree(t, root))
	assertNoTempFiles(t, root)
}

// Unsafe paths are refused before any file in the batch is touched.
func TestApply_UnsafePathAbortsBeforeMutation(t *testing.T) {
	root := newWorktree(t, map[string]string{"a.go": "package a\n"})
	w := newWriter(t, root)
	before := snapshotTree(t, root)

	_, err := w.Apply(context.Background(), []FileOp{
		{Kind: OpWrite, Path: "a.go", Content: "package a\n\nvar X = 1\n"},
		{Kind: OpWrite, Path: "../escaped.go", Content: "escaped"},
	})
	assertRejected(t, err, ReasonPathTraversal,
		filepath.Join(filepath.Dir(root), "escaped.go"))

	assertTreeEqual(t, before, snapshotTree(t, root))
}
