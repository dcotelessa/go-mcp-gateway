package filewriter

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	fenceMarker      = "```"
	infoPrefixFile   = "file:"
	infoPrefixDelete = "file-delete:"
)

// Parser turns a model response into an ordered list of FileOps.
//
// Only fences whose info string begins with file: or file-delete: are
// operations. Every other fence — a plain ```go block, for instance — is
// ignored entirely, as is all prose.
//
// Parser is pure: it holds no state and never touches the filesystem.
type Parser struct{}

// NewParser returns a Parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse scans output and returns operations in the order their blocks appear.
//
// Returns a *ParseError for any input that yields no usable operations. It
// never returns zero operations with a nil error.
func (p *Parser) Parse(output string) ([]FileOp, error) {
	lines := strings.Split(output, "\n")

	var ops []FileOp
	seen := make(map[string]int) // path -> 1-based line of first occurrence

	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		if !strings.HasPrefix(trimmed, fenceMarker) {
			i++
			continue
		}

		info := strings.TrimSpace(strings.TrimPrefix(trimmed, fenceMarker))
		openLine := i + 1 // 1-based

		kind, rawPath, isOp := classifyInfo(info)
		if !isOp {
			// Not an operation fence — skip to its closing fence so any
			// backticks inside it can't be mistaken for operations.
			i = skipToClose(lines, i+1)
			continue
		}

		if rawPath == "" {
			return nil, &ParseError{
				Reason: ReasonMissingPath,
				Detail: fmt.Sprintf("info string was %q", info),
				Line:   openLine,
			}
		}

		body, closeIdx, closed := collectBody(lines, i+1)
		if !closed {
			return nil, &ParseError{
				Reason: ReasonUnclosedFence,
				Detail: fmt.Sprintf("block for %q was never closed", rawPath),
				Line:   openLine,
			}
		}

		cleanPath := filepath.Clean(rawPath)
		if prev, dup := seen[cleanPath]; dup {
			return nil, &ParseError{
				Reason: ReasonDuplicatePath,
				Detail: fmt.Sprintf("%q first appeared at line %d", cleanPath, prev),
				Line:   openLine,
			}
		}
		seen[cleanPath] = openLine

		op := FileOp{Kind: kind, RawPath: rawPath, Path: cleanPath}
		if kind == OpWrite {
			// An empty body is a legitimate empty file, not an error.
			op.Content = body
		}
		ops = append(ops, op)
		i = closeIdx + 1
	}

	if len(ops) == 0 {
		return nil, &ParseError{
			Reason: ReasonNoOperations,
			Detail: "output contained no file: or file-delete: fences",
		}
	}

	return ops, nil
}

// classifyInfo reads a fence info string and reports whether it opens an
// operation block, which kind, and the raw path it names.
func classifyInfo(info string) (kind OpKind, rawPath string, isOp bool) {
	switch {
	case strings.HasPrefix(info, infoPrefixDelete):
		return OpDelete, strings.TrimSpace(strings.TrimPrefix(info, infoPrefixDelete)), true
	case strings.HasPrefix(info, infoPrefixFile):
		return OpWrite, strings.TrimSpace(strings.TrimPrefix(info, infoPrefixFile)), true
	default:
		return "", "", false
	}
}

// collectBody gathers lines from start until the closing fence.
//
// Returns the body with the trailing newline before the closing fence
// removed, the index of the closing fence line, and whether it was found.
func collectBody(lines []string, start int) (body string, closeIdx int, closed bool) {
	var collected []string
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fenceMarker {
			return strings.Join(collected, "\n"), i, true
		}
		collected = append(collected, lines[i])
	}
	return "", len(lines), false
}

// skipToClose returns the index just past the next closing fence, or the end
// of input if none is found. Used to step over non-operation fences.
func skipToClose(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fenceMarker {
			return i + 1
		}
	}
	return len(lines)
}
