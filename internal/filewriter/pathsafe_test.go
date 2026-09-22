package filewriter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newWorktree builds a temp worktree with the given files and returns its
// canonical path (t.TempDir can sit under a symlinked /tmp).
func newWorktree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval root: %v", err)
	}
	return canonical
}

func newValidator(t *testing.T, root string) *PathValidator {
	t.Helper()
	v, err := NewPathValidator(root, SafetyConfig{})
	if err != nil {
		t.Fatalf("NewPathValidator: %v", err)
	}
	return v
}

func writeErr(t *testing.T, err error) *WriteError {
	t.Helper()
	var we *WriteError
	if !errors.As(err, &we) {
		t.Fatalf("expected *WriteError, got %T: %v", err, err)
	}
	return we
}

// assertRejected checks the reason and that nothing was created anywhere.
func assertRejected(t *testing.T, err error, wantReason string, mustNotExist ...string) {
	t.Helper()
	if we := writeErr(t, err); we.Reason != wantReason {
		t.Errorf("reason = %q, want %q", we.Reason, wantReason)
	}
	for _, p := range mustNotExist {
		if _, statErr := os.Lstat(p); statErr == nil {
			t.Errorf("validation created or touched %q", p)
		}
	}
}

// SAFE-1 — Relative traversal rejected before disk.
func TestResolve_RejectsRelativeTraversal(t *testing.T) {
	root := newWorktree(t, nil)
	outside := filepath.Join(filepath.Dir(root), "escaped.txt")

	_, _, err := newValidator(t, root).Resolve("../escaped.txt")
	assertRejected(t, err, ReasonPathTraversal, outside)
}

// SAFE-2 — Absolute path outside worktree rejected.
func TestResolve_RejectsOutsideAbsolute(t *testing.T) {
	root := newWorktree(t, nil)

	_, _, err := newValidator(t, root).Resolve("/etc/hosts")
	assertRejected(t, err, ReasonPathTraversal)
}

// SAFE-3 — Traversal through a valid prefix rejected.
func TestResolve_RejectsPrefixTraversal(t *testing.T) {
	root := newWorktree(t, map[string]string{"internal/a.go": "package a\n"})
	outside := filepath.Join(filepath.Dir(root), "escaped.go")

	_, _, err := newValidator(t, root).Resolve("internal/../../escaped.go")
	assertRejected(t, err, ReasonPathTraversal, outside)
}

// SAFE-4 — Absolute path inside worktree accepted and normalized.
func TestResolve_AcceptsInsideAbsolute(t *testing.T) {
	root := newWorktree(t, map[string]string{"internal/config/config.go": "package config\n"})

	rel, abs, err := newValidator(t, root).Resolve(filepath.Join(root, "internal/config/config.go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != filepath.Join("internal", "config", "config.go") {
		t.Errorf("rel = %q", rel)
	}
	if abs != filepath.Join(root, "internal", "config", "config.go") {
		t.Errorf("abs = %q", abs)
	}
}

// SAFE-5 — Gitignored path rejected.
func TestResolve_RejectsGitignored(t *testing.T) {
	root := newWorktree(t, map[string]string{".gitignore": "dist/\n*.log\n"})
	v := newValidator(t, root)

	_, _, err := v.Resolve("dist/bundle.js")
	assertRejected(t, err, ReasonGitignored)

	_, _, err = v.Resolve("internal/debug.log")
	assertRejected(t, err, ReasonGitignored)

	if _, _, err := v.Resolve("internal/keep.go"); err != nil {
		t.Errorf("unignored path rejected: %v", err)
	}
}

// Negation re-includes a path; the last matching rule wins.
func TestResolve_GitignoreNegation(t *testing.T) {
	root := newWorktree(t, map[string]string{".gitignore": "*.env\n!keep.env\n"})
	v := newValidator(t, root)

	if _, _, err := v.Resolve("secrets.env"); err == nil {
		t.Error("expected secrets.env to be ignored")
	}
	if _, _, err := v.Resolve("keep.env"); err != nil {
		t.Errorf("negated pattern should be allowed: %v", err)
	}
}

// SAFE-6 — Binary extension rejected.
func TestResolve_RejectsBinaryExtension(t *testing.T) {
	root := newWorktree(t, nil)
	v := newValidator(t, root)

	for _, p := range []string{"assets/logo.png", "assets/LOGO.PNG", "dist/app.so"} {
		_, _, err := v.Resolve(p)
		assertRejected(t, err, ReasonBinaryExtension)
	}
}

// SAFE-7 — Symlink escape rejected.
func TestResolve_RejectsSymlinkEscape(t *testing.T) {
	root := newWorktree(t, nil)
	if err := os.Symlink("/etc", filepath.Join(root, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, _, err := newValidator(t, root).Resolve("out/hosts")
	assertRejected(t, err, ReasonPathTraversal)
}

// A symlink that stays inside the worktree is fine.
func TestResolve_AcceptsInternalSymlink(t *testing.T) {
	root := newWorktree(t, map[string]string{"real/a.go": "package a\n"})
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, abs, err := newValidator(t, root).Resolve("link/a.go")
	if err != nil {
		t.Fatalf("internal symlink rejected: %v", err)
	}
	if abs != filepath.Join(root, "real", "a.go") {
		t.Errorf("abs = %q, want the resolved path under real/", abs)
	}
}

// A dangling symlink is ambiguity: reject rather than allow.
func TestResolve_RejectsDanglingSymlink(t *testing.T) {
	root := newWorktree(t, nil)
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "dangling")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, _, err := newValidator(t, root).Resolve("dangling/file.go")
	assertRejected(t, err, ReasonPathTraversal)
}

// SAFE-8 — Valid relative path resolves.
func TestResolve_ValidRelativePath(t *testing.T) {
	root := newWorktree(t, nil)

	rel, abs, err := newValidator(t, root).Resolve("internal/rest/implement.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != filepath.Join("internal", "rest", "implement.go") {
		t.Errorf("rel = %q", rel)
	}
	if abs != filepath.Join(root, "internal", "rest", "implement.go") {
		t.Errorf("abs = %q", abs)
	}
}

// SAFE-9 — Invalid worktree root.
func TestNewPathValidator_InvalidRoot(t *testing.T) {
	if _, err := NewPathValidator(filepath.Join(t.TempDir(), "nope"), SafetyConfig{}); err == nil {
		t.Error("expected error for a nonexistent root")
	}

	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPathValidator(file, SafetyConfig{}); err == nil {
		t.Error("expected error for a regular-file root")
	}

	if _, err := NewPathValidator("", SafetyConfig{}); err == nil {
		t.Error("expected error for an empty root")
	}
}

func TestResolve_RejectsEmptyAndDot(t *testing.T) {
	v := newValidator(t, newWorktree(t, nil))

	for _, p := range []string{"", "   ", ".", "./"} {
		if _, _, err := v.Resolve(p); err == nil {
			t.Errorf("expected rejection for %q", p)
		}
	}
}

// SAFE-10 — Configured extensions extend the defaults and cannot shrink them.
func TestSafetyConfig_ExtensionsExtendDefaults(t *testing.T) {
	root := newWorktree(t, nil)
	v, err := NewPathValidator(root, SafetyConfig{BinaryExtensions: []string{".wasm", ".WEBP"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"build/app.wasm", "assets/hero.webp"} {
		if _, _, err := v.Resolve(p); err == nil {
			t.Errorf("expected %q to be rejected", p)
		}
	}
	// A default is still refused even though the config did not list it.
	_, _, err = v.Resolve("assets/logo.png")
	assertRejected(t, err, ReasonBinaryExtension)
}

// An empty config still refuses every default extension.
func TestSafetyConfig_EmptyKeepsDefaults(t *testing.T) {
	v := newValidator(t, newWorktree(t, nil))
	for _, ext := range DefaultBinaryExtensions {
		_, _, err := v.Resolve("assets/file" + ext)
		assertRejected(t, err, ReasonBinaryExtension)
	}
}
