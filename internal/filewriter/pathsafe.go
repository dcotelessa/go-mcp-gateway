package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultBinaryExtensions are refused by the validator. Writing them from a
// model response is never intended: they cannot be reviewed in a diff and a
// full-replacement contract cannot represent them.
var DefaultBinaryExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".ico", ".pdf",
	".zip", ".tar", ".gz", ".exe", ".dll", ".so", ".dylib",
	".bin", ".woff", ".woff2", ".ttf",
}

// SafetyConfig configures path validation.
type SafetyConfig struct {
	// BinaryExtensions are refused in addition to DefaultBinaryExtensions,
	// compared case-insensitively. The defaults cannot be removed: a
	// configuration mistake should never widen what the gateway is willing
	// to write.
	BinaryExtensions []string
}

// PathValidator confines every operation to one worktree.
//
// It is the security boundary of the package: no path reaches the
// filesystem without passing through Resolve, and Resolve rejects anything
// it cannot prove lies inside the worktree.
type PathValidator struct {
	// root is the worktree, absolute and with symlinks already resolved,
	// so containment comparisons are made against a canonical path.
	root string

	binaryExt map[string]struct{}
	ignores   *ignoreList
}

// NewPathValidator returns a validator rooted at worktree.
//
// Construction fails if the worktree does not exist, is not a directory, or
// cannot be canonicalized — there is no safe way to validate paths against
// a root that cannot itself be resolved.
func NewPathValidator(worktree string, cfg SafetyConfig) (*PathValidator, error) {
	if strings.TrimSpace(worktree) == "" {
		return nil, fmt.Errorf("filewriter: worktree path is empty")
	}

	abs, err := filepath.Abs(worktree)
	if err != nil {
		return nil, fmt.Errorf("filewriter: worktree %q: %w", worktree, err)
	}

	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("filewriter: worktree %q: %w", worktree, err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("filewriter: worktree %q: %w", worktree, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("filewriter: worktree %q is not a directory", worktree)
	}

	// Configured extensions extend the defaults; they never replace them.
	set := make(map[string]struct{}, len(DefaultBinaryExtensions)+len(cfg.BinaryExtensions))
	for _, e := range DefaultBinaryExtensions {
		set[strings.ToLower(e)] = struct{}{}
	}
	for _, e := range cfg.BinaryExtensions {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			set[e] = struct{}{}
		}
	}

	ignores, err := loadIgnoreList(root)
	if err != nil {
		// An unreadable .gitignore is ambiguity: fail construction rather
		// than silently validating without it.
		return nil, fmt.Errorf("filewriter: reading .gitignore: %w", err)
	}

	return &PathValidator{root: root, binaryExt: set, ignores: ignores}, nil
}

// Root returns the canonical worktree path.
func (v *PathValidator) Root() string { return v.root }

// Resolve validates raw and returns its worktree-relative and absolute
// forms. It never touches the filesystem outside the worktree, and it
// performs no mutation.
//
// Rejections are *WriteError with reason path_traversal, gitignored, or
// binary_extension.
func (v *PathValidator) Resolve(raw string) (rel string, abs string, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", &WriteError{
			Reason: ReasonPathTraversal,
			Path:   raw,
			Detail: "empty path",
		}
	}

	rel, err = v.relative(raw)
	if err != nil {
		return "", "", err
	}

	// Symlink-aware containment. Done before any extension or ignore check
	// so an escaping path is reported as traversal regardless of its name.
	abs, err = v.resolveWithin(rel, raw)
	if err != nil {
		return "", "", err
	}

	if _, bad := v.binaryExt[strings.ToLower(filepath.Ext(rel))]; bad {
		return "", "", &WriteError{
			Reason: ReasonBinaryExtension,
			Path:   rel,
			Detail: "extension is configured as binary",
		}
	}

	if v.ignores.Match(rel) {
		return "", "", &WriteError{
			Reason: ReasonGitignored,
			Path:   rel,
			Detail: "path matches .gitignore",
		}
	}

	return rel, abs, nil
}

// relative reduces raw to a clean worktree-relative path, rejecting
// anything that lexically escapes the worktree.
func (v *PathValidator) relative(raw string) (string, error) {
	traversal := func(detail string) error {
		return &WriteError{Reason: ReasonPathTraversal, Path: raw, Detail: detail}
	}

	var rel string
	if filepath.IsAbs(raw) {
		r, err := filepath.Rel(v.root, filepath.Clean(raw))
		if err != nil {
			return "", traversal("absolute path is not relative to the worktree")
		}
		rel = r
	} else {
		rel = filepath.Clean(raw)
	}

	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", traversal("path escapes the worktree")
	}
	if filepath.IsAbs(rel) {
		return "", traversal("path escapes the worktree")
	}
	return rel, nil
}

// resolveWithin resolves symlinks as far as the path exists and verifies
// the result lies inside the worktree.
//
// The target itself usually does not exist — every create writes a new
// file — so EvalSymlinks cannot be applied to it directly. Instead the
// deepest existing ancestor is canonicalized and the missing components are
// rejoined. Any resolution error other than "does not exist" is treated as
// a rejection: ambiguity never yields an allow.
func (v *PathValidator) resolveWithin(rel, raw string) (string, error) {
	traversal := func(detail string) error {
		return &WriteError{Reason: ReasonPathTraversal, Path: rel, Detail: detail}
	}

	target := filepath.Join(v.root, rel)

	probe := target
	var missing []string
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", traversal("ancestor could not be inspected")
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", traversal("no existing ancestor")
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}

	resolvedProbe, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", traversal("ancestor could not be resolved")
	}
	if !v.contains(resolvedProbe) {
		return "", traversal("resolves outside the worktree")
	}

	// missing was collected leaf-first; rejoin root-first.
	parts := []string{resolvedProbe}
	for i := len(missing) - 1; i >= 0; i-- {
		parts = append(parts, missing[i])
	}
	final := filepath.Join(parts...)

	if !v.contains(final) {
		return "", traversal("resolves outside the worktree")
	}
	return final, nil
}

// contains reports whether p is the worktree root or lies beneath it.
func (v *PathValidator) contains(p string) bool {
	if p == v.root {
		return true
	}
	return strings.HasPrefix(p, v.root+string(filepath.Separator))
}
