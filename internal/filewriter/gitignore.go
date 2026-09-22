package filewriter

import (
	"os"
	"path/filepath"
	"strings"
)

// ignoreList matches worktree-relative paths against .gitignore patterns.
//
// It implements a documented subset of gitignore semantics, chosen to cover
// the patterns that appear in real .gitignore files without pulling in a
// dependency:
//
//   - blank lines and # comments are skipped
//   - a leading ! negates; the last matching pattern wins
//   - a leading / anchors the pattern to the worktree root
//   - a trailing / restricts the pattern to directories, which for our
//     purposes means it matches any path beneath that directory
//   - * and ? match within one path segment; ** matches across segments
//   - a pattern without any / matches the basename of any segment
//
// Not supported: character classes with ranges beyond what filepath.Match
// handles, and nested .gitignore files in subdirectories. A path this
// matcher fails to recognize is simply not excluded — so this is a
// convenience filter, not a security control. Path containment is what
// keeps writes inside the worktree.
type ignoreList struct {
	rules []ignoreRule
}

type ignoreRule struct {
	pattern  string // normalized, without leading ! or /
	negate   bool
	anchored bool
	dirOnly  bool
	hasSlash bool
}

// loadIgnoreList reads <root>/.gitignore. A missing file yields an empty
// list; an unreadable one is reported so the caller can fail closed.
func loadIgnoreList(root string) (*ignoreList, error) {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return &ignoreList{}, nil
		}
		return nil, err
	}

	var list ignoreList
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var r ignoreRule
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		r.pattern = line
		r.hasSlash = strings.Contains(line, "/")
		list.rules = append(list.rules, r)
	}
	return &list, nil
}

// Match reports whether a worktree-relative path is ignored. The last
// matching rule wins, so a later negation can re-include a path.
func (l *ignoreList) Match(rel string) bool {
	if l == nil || len(l.rules) == 0 {
		return false
	}
	rel = filepath.ToSlash(rel)
	segments := strings.Split(rel, "/")

	ignored := false
	for _, r := range l.rules {
		if r.matches(rel, segments) {
			ignored = !r.negate
		}
	}
	return ignored
}

func (r ignoreRule) matches(rel string, segments []string) bool {
	// A dirOnly pattern matches anything beneath the directory, so test
	// each ancestor prefix as well as the path itself.
	candidates := []string{rel}
	if r.dirOnly {
		candidates = candidates[:0]
		for i := 1; i < len(segments); i++ {
			candidates = append(candidates, strings.Join(segments[:i], "/"))
		}
	}

	for _, c := range candidates {
		if r.hasSlash || r.anchored {
			if globMatch(r.pattern, c) {
				return true
			}
			continue
		}
		// Unanchored, no slash: match the basename of any segment.
		for _, seg := range strings.Split(c, "/") {
			if globMatch(r.pattern, seg) {
				return true
			}
		}
	}
	return false
}

// globMatch applies filepath.Match semantics, extended so that ** spans
// path separators.
func globMatch(pattern, name string) bool {
	if !strings.Contains(pattern, "**") {
		ok, err := filepath.Match(pattern, name)
		return err == nil && ok
	}

	parts := strings.Split(pattern, "**")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		switch i {
		case 0:
			if !strings.HasPrefix(name[pos:], part) {
				return false
			}
			pos += len(part)
		default:
			idx := strings.Index(name[pos:], part)
			if idx < 0 {
				return false
			}
			pos += idx + len(part)
		}
	}
	// A trailing ** matches the remainder; otherwise the last literal
	// segment must have reached the end.
	if strings.HasSuffix(pattern, "**") {
		return true
	}
	return pos == len(name)
}
