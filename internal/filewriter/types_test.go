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
		name string
		err  *ParseError
	}{
		{"no line number", &ParseError{Reason: ReasonNoOperations, Detail: "output contained no file fences"}},
		{"with line number", &ParseError{Reason: ReasonUnclosedFence, Detail: "block opened at types.go", Line: 42}},
		{"missing path", &ParseError{Reason: ReasonMissingPath, Detail: "info string was 'file:'", Line: 7}},
		{"duplicate path", &ParseError{Reason: ReasonDuplicatePath, Detail: "internal/a.go appeared twice"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if !strings.Contains(msg, tc.err.Reason) {
				t.Errorf("message %q does not contain reason %q", msg, tc.err.Reason)
			}
			if !strings.Contains(msg, tc.err.Detail) {
				t.Errorf("message %q does not contain detail %q", msg, tc.err.Detail)
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
	want := map[OpKind]string{
		OpWrite:  "write",
		OpCreate: "create",
		OpModify: "modify",
		OpDelete: "delete",
	}
	for k, v := range want {
		if string(k) != v {
			t.Errorf("OpKind %q changed, want %q", k, v)
		}
	}
}
