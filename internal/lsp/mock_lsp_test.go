package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
)

// mockLSPBinary compiles a small Go program that speaks JSON-RPC over stdio.
// It responds to initialize and shutdown; all other requests get no response
// (used to test timeout behavior).
func mockLSPBinary(t *testing.T) string {
	t.Helper()

	src := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type msg struct {
	JSONRPC string          ` + "`" + `json:"jsonrpc"` + "`" + `
	ID      int64           ` + "`" + `json:"id,omitempty"` + "`" + `
	Method  string          ` + "`" + `json:"method,omitempty"` + "`" + `
	Result  json.RawMessage ` + "`" + `json:"result,omitempty"` + "`" + `
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	for {
		var length int
		for {
			line, err := reader.ReadString('\n')
			if err != nil { os.Exit(0) }
			line = strings.TrimSpace(line)
			if line == "" { break }
			if strings.HasPrefix(line, "Content-Length:") {
				parts := strings.SplitN(line, ":", 2)
				length, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		}
		if length == 0 { continue }
		body := make([]byte, length)
		if _, err := reader.Read(body); err != nil { os.Exit(0) }

		var req msg
		if err := json.Unmarshal(body, &req); err != nil { continue }

		// Only respond to requests (have ID), not notifications
		if req.ID == 0 { continue }

		var result json.RawMessage
		switch req.Method {
		case "initialize":
			result = json.RawMessage(` + "`" + `{"capabilities":{}}` + "`" + `)
		case "shutdown":
			result = json.RawMessage(` + "`" + `null` + "`" + `)
		default:
			// No response — used to test timeout behavior
			continue
		}

		resp, _ := json.Marshal(msg{JSONRPC: "2.0", ID: req.ID, Result: result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(resp), resp)
	}
}
`
	dir := t.TempDir()
	srcFile := dir + "/main.go"
	binFile := dir + "/mock-lsp"

	if err := os.WriteFile(srcFile, []byte(src), 0644); err != nil {
		t.Fatalf("write mock LSP source: %v", err)
	}

	cmd := exec.Command("go", "build", "-o", binFile, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile mock LSP: %v\n%s", err, out)
	}

	return binFile
}

// sessionWithMock creates a Session wired to the mock LSP binary.
func sessionWithMock(t *testing.T) *Session {
	t.Helper()
	bin := mockLSPBinary(t)

	cfg := DefaultConfig()
	cfg.RequestTimeoutSec = 5
	cfg.InitTimeoutSec = 10
	sc := ServerConfig{
		Command:  bin,
		Args:     []string{},
		Language: "go",
	}

	s := newSession(sc, t.TempDir(), cfg)
	s.cmd = exec.Command(bin)
	s.cmd.Env = os.Environ()

	var err error
	s.stdin, err = s.cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	s.stdout = bufio.NewReader(stdout)

	if err := s.cmd.Start(); err != nil {
		t.Fatalf("start mock LSP: %v", err)
	}

	t.Cleanup(func() {
		s.kill()
		_ = s.cmd.Wait()
	})

	go s.readLoop()
	go s.initialize()

	return s
}

// writeContentLength writes a Content-Length framed JSON-RPC message to w.
func writeContentLength(w io.Writer, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}
