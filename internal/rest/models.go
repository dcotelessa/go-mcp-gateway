package rest

// ClassifyRequest is the body for POST /classify.
type ClassifyRequest struct {
	Task string `json:"task"`
}

// ClassifyResponse is the body returned by POST /classify.
type ClassifyResponse struct {
	Complexity string `json:"complexity"`
	QALevel    string `json:"qaLevel"`
}

// ImplementRequest is the body for POST /implement.
// Field names match the Mastra task-execution-workflow.ts contract exactly.
type ImplementRequest struct {
	Task          string   `json:"task"`
	Files         []string `json:"files"`
	ReasoningTags []string `json:"reasoningTags"`
}

// ImplementResponse is the body returned by POST /implement.
type ImplementResponse struct {
	FilesChanged  []string `json:"files_changed"`
	Status        string   `json:"status"`
	ReasoningTags []string `json:"reasoning_tags"`
	Content       string   `json:"content,omitempty"`
	TotalTokens   int      `json:"total_tokens,omitempty"`
}

// InterpretRequest is the body for POST /interpret.
type InterpretRequest struct {
	VitestOutput string   `json:"vitestOutput"`
	Diff         string   `json:"diff"`
	Scenarios    []string `json:"scenarios"`
}

// InterpretResponse is the body returned by POST /interpret.
// Field names match the Mastra QAVerdictSchema exactly.
type InterpretResponse struct {
	TaskID     string   `json:"taskId"`
	Status     string   `json:"status"`
	Failures   []string `json:"failures"`
	Hint       string   `json:"hint,omitempty"`
	NextAction string   `json:"nextAction"`
}

// ErrorResponse is the standard error envelope for all 4xx/5xx responses.
// All error responses use this shape — never raw strings (REQ-REST-015).
type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}
