package filewriter

import (
	"strings"
	"testing"
)

// PARSE-10 — ParseError message quality.
// GIVEN a *ParseError with any Reason and Detail
// WHEN Error() is called
// THEN the message contains both the Reason token and the Detail text.
func TestParseError_MessageContainsReasonAndDetail(t *testing.T) {
	cases := []struct {
		name   string
		err    *ParseError
		reason string
		detail string
	}{
		{
			name:   "no line number",
			err:    &ParseError{Reason: ReasonNoOperations, Detail: "output contained no file fences"},
			reason: ReasonNoOperations,
			detail: "output contained no file fences",
		},
		{
			name:   "with line number",
			err:    &ParseError{Reason: ReasonUnclosedFence, Detail: "block opened at types.go", Line: 42},
			reason: ReasonUnclosedFence,
			detail: "block opened at types.go",
		},
		{
			name:   "missing path",
			err:    &ParseError{Reason: ReasonMissingPath, Detail: "info string was 'file:'", Line: 7},
			reason: ReasonMissingPath,
			detail: "info string was 'file:'",
		},
		{
			name:   "empty content",
			err:    &ParseError{Reason: ReasonEmptyContent, Detail: "body was empty for main.go"},
			reason: ReasonEmptyContent,
			detail: "body was empty for main.go",
		},
		{
			name:   "duplicate path",
			err:    &ParseError{Reason: ReasonDuplicatePath, Detail: "internal/a.go appeared twice"},
			reason: ReasonDuplicatePath,
			detail: "internal/a.go appeared twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if !strings.Contains(msg, tc.reason) {
				t.Errorf("message %q does not contain reason %q", msg, tc.reason)
			}
			if !strings.Contains(msg, tc.detail) {
				t.Errorf("message %q does not contain detail %q", msg, tc.detail)
			}
		})
	}
}

func TestParseError_IncludesLineWhenPresent(t *testing.T) {
	withLine := (&ParseError{Reason: ReasonUnclosedFence, Detail: "d", Line: 12}).Error()
	if !strings.Contains(withLine, "12") {
		t.Errorf("expected line number in %q", withLine)
	}

	withoutLine := (&ParseError{Reason: ReasonUnclosedFence, Detail: "d"}).Error()
	if strings.Contains(withoutLine, "line") {
		t.Errorf("did not expect line reference in %q", withoutLine)
	}
}

func TestOpKindConstants(t *testing.T) {
	if OpCreate != "create" || OpModify != "modify" || OpDelete != "delete" {
		t.Errorf("OpKind constants changed: %q %q %q", OpCreate, OpModify, OpDelete)
	}
}
