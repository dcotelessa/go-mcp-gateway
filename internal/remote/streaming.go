package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// doStream sends a streaming request and aggregates the SSE chunks.
func (c *baseClient) doStream(ctx context.Context, req RemoteRequest) (RemoteResult, int, error) {
	messages := buildMessages(req)

	payload := openAIRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	}
	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "marshal: " + err.Error()}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "build request: " + err.Error()}
	}

	c.setHeaders(httpReq)

	client := &http.Client{Timeout: c.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return RemoteResult{}, 0, &TerminalError{Provider: c.name, Message: "http: " + err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return RemoteResult{}, resp.StatusCode, nil
	}

	if resp.StatusCode != http.StatusOK {
		return RemoteResult{}, resp.StatusCode, &TerminalError{
			Provider:   c.name,
			StatusCode: resp.StatusCode,
		}
	}

	return c.readSSE(resp, req)
}

// readSSE reads and aggregates Server-Sent Events from a streaming response.
func (c *baseClient) readSSE(resp *http.Response, req RemoteRequest) (RemoteResult, int, error) {
	var (
		contentBuf       strings.Builder
		promptTokens     int
		completionTokens int
		model            string
	)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE lines start with "data: "
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Stream terminator
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		if model == "" {
			model = chunk.Model
		}

		// Aggregate content deltas
		if len(chunk.Choices) > 0 {
			contentBuf.WriteString(chunk.Choices[0].Delta.Content)
		}

		// Usage appears in the final chunk for some providers
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return RemoteResult{}, resp.StatusCode, &TerminalError{
			Provider: c.name,
			Message:  "stream read: " + err.Error(),
		}
	}

	return RemoteResult{
		Content:          contentBuf.String(),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Model:            model,
		Provider:         c.name,
	}, resp.StatusCode, nil
}

// suppress unused imports
var (
	_ = fmt.Sprintf
	_ = time.Second
)
