package router

import "strings"

// classifyTask applies keyword heuristics to map a task description
// to a Complexity and QALevel.
func classifyTask(task string) (Complexity, QALevel) {
	lower := strings.ToLower(task)

	// Text operations — rename, reformat, comment, move
	textOpKeywords := []string{"rename", "reformat", "comment", "move file", "text replace", "text_op"}
	for _, kw := range textOpKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityTextOp, QALevelSkip
		}
	}

	// Scaffold — new file, stub, boilerplate, skeleton
	scaffoldKeywords := []string{"scaffold", "stub", "skeleton", "boilerplate", "new file", "create file", "initialize"}
	for _, kw := range scaffoldKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityScaffold, QALevelSkip
		}
	}

	// Recovery — fix, repair, failing test, retry
	recoveryKeywords := []string{"fix", "repair", "failing test", "recovery", "retry", "broken"}
	for _, kw := range recoveryKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityRecovery, QALevelFull
		}
	}

	// Multi-file — multiple files, cross-package, interface, coordinate
	multiFileKeywords := []string{"multi", "multiple file", "cross-package", "interface", "coordinate", "across"}
	for _, kw := range multiFileKeywords {
		if strings.Contains(lower, kw) {
			return ComplexityMultiFile, QALevelFull
		}
	}

	// Default — single file implementation
	return ComplexitySingleFile, QALevelFull
}

// interpretOutput parses vitest stdout to produce a QAVerdict.
func interpretOutput(vitestOutput, _ string, _ []string) QAVerdict {
	lower := strings.ToLower(vitestOutput)

	// Detect explicit pass signal
	if strings.Contains(lower, "all tests passed") ||
		strings.Contains(lower, "tests passed") &&
			!strings.Contains(lower, "failed") {
		return QAVerdict{
			Status:     "pass",
			Failures:   []string{},
			NextAction: "merge",
		}
	}

	// Detect failure signal
	if strings.Contains(lower, "failed") ||
		strings.Contains(lower, "error") {
		failures := extractFailures(vitestOutput)
		return QAVerdict{
			Status:     "fail",
			Failures:   failures,
			NextAction: "retry",
		}
	}

	// Inconclusive — output doesn't clearly indicate pass or fail
	return QAVerdict{
		Status:     "inconclusive",
		Failures:   []string{},
		NextAction: "escalate",
	}
}

// extractFailures pulls failed test names from vitest output.
func extractFailures(output string) []string {
	var failures []string
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "× ") ||
			strings.Contains(lower, "fail") ||
			strings.Contains(lower, "error:") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				failures = append(failures, trimmed)
			}
		}
	}
	if failures == nil {
		failures = []string{}
	}
	return failures
}
