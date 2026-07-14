package lsp

import (
	"fmt"
	"strings"
)

// ErrUnsupportedLanguage is returned when no LSP binary is configured
// for the requested language.
type ErrUnsupportedLanguage struct {
	Language  string
	Supported []string
}

func (e *ErrUnsupportedLanguage) Error() string {
	return fmt.Sprintf("lsp: unsupported language %q (supported: %s)",
		e.Language, strings.Join(e.Supported, ", "))
}

// ErrBinaryNotFound is returned when the LSP binary is not installed.
type ErrBinaryNotFound struct {
	Language     string
	ExpectedPath string
	InstallHint  string
}

func (e *ErrBinaryNotFound) Error() string {
	return fmt.Sprintf("lsp: binary not found for %q at %q — %s",
		e.Language, e.ExpectedPath, e.InstallHint)
}

// ErrInitTimeout is returned when LSP initialization exceeds the timeout.
type ErrInitTimeout struct {
	Language      string
	WorkspaceRoot string
	TimeoutSec    int
}

func (e *ErrInitTimeout) Error() string {
	return fmt.Sprintf("lsp: init timeout after %ds for %s in %s",
		e.TimeoutSec, e.Language, e.WorkspaceRoot)
}

// ErrRequestTimeout is returned when an LSP request exceeds the per-request timeout.
type ErrRequestTimeout struct {
	Method     string
	TimeoutSec int
}

func (e *ErrRequestTimeout) Error() string {
	return fmt.Sprintf("lsp: request timeout after %ds for method %q",
		e.TimeoutSec, e.Method)
}

// ErrProcessCrashed is returned when the LSP process exits unexpectedly.
type ErrProcessCrashed struct {
	Language string
	ExitCode int
}

func (e *ErrProcessCrashed) Error() string {
	return fmt.Sprintf("lsp: process crashed for %s with exit code %d",
		e.Language, e.ExitCode)
}

// ErrFileNotFound is returned when a requested file does not exist on disk.
type ErrFileNotFound struct {
	Path string
}

func (e *ErrFileNotFound) Error() string {
	return fmt.Sprintf("lsp: file not found: %q", e.Path)
}
