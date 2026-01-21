package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLSPDiagnosticsTool_WithFakeServer(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewLSPDiagnosticsTool(root)

	serverCmd := os.Args[0]
	serverArgs := []interface{}{"-test.run=TestFakeLSPServer", "-test.v"}

	t.Setenv("AE_FAKE_LSP_SERVER", "1")

	args := map[string]interface{}{
		"server":      serverCmd,
		"args":        serverArgs,
		"files":       []interface{}{"main.go"},
		"timeout_ms":  1500,
		"language_id": "go",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("expected success, got: %s (%s)", res.Status, res.Error)
	}

	var report struct {
		Summary struct {
			TotalDiagnostics int `json:"total_diagnostics"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(res.Content), &report); err != nil {
		t.Fatalf("failed to parse report: %v", err)
	}
	if report.Summary.TotalDiagnostics == 0 {
		t.Fatalf("expected diagnostics from fake server")
	}
}

func TestFakeLSPServer(t *testing.T) {
	if os.Getenv("AE_FAKE_LSP_SERVER") != "1" {
		t.Skip("helper process")
	}

	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	send := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
		_, _ = w.Write(data)
		_ = w.Flush()
	}

	var openedURI string

	for {
		msg, err := readLSPMessage(r)
		if err != nil {
			os.Exit(0)
		}

		var env lspEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		switch env.Method {
		case "initialize":
			send(map[string]any{
				"jsonrpc": "2.0",
				"id":      env.ID,
				"result": map[string]any{
					"capabilities": map[string]any{},
				},
			})
		case "initialized":
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(env.Params, &params)
			openedURI = params.TextDocument.URI

			send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri": openedURI,
					"diagnostics": []map[string]any{
						{
							"range": map[string]any{
								"start": map[string]any{"line": 0, "character": 0},
								"end":   map[string]any{"line": 0, "character": 5},
							},
							"severity": 1,
							"source":   "fake",
							"message":  "fake error",
						},
					},
				},
			})
		case "shutdown":
			send(map[string]any{
				"jsonrpc": "2.0",
				"id":      env.ID,
				"result":  nil,
			})
		case "exit":
			os.Exit(0)
		}
	}
}
