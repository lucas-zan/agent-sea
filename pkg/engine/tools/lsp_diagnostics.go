package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
)

type LSPDiagnosticsTool struct {
	BaseTool
	workspaceRoot string
}

func NewLSPDiagnosticsTool(workspaceRoot string) *LSPDiagnosticsTool {
	return &LSPDiagnosticsTool{
		BaseTool: NewBaseTool(
			"lsp_diagnostics",
			"Run an LSP server (stdio) and collect diagnostics for the given files. This is useful for compiler/type errors (e.g., gopls for Go).",
			[]ParameterDef{
				{Name: "server", Type: "string", Description: "LSP server command (default: gopls)", Required: false},
				{Name: "args", Type: "array", Description: "LSP server args (e.g., [\"--stdio\"])", Required: false},
				{Name: "files", Type: "array", Description: "Files to open and collect diagnostics for (paths relative to workspace)", Required: true},
				{Name: "timeout_ms", Type: "integer", Description: "Overall timeout in milliseconds (default: 4000)", Required: false},
				{Name: "language_id", Type: "string", Description: "Optional LSP languageId for didOpen (default: inferred from extension)", Required: false},
			},
			api.RiskHigh,
		),
		workspaceRoot: workspaceRoot,
	}
}

func (t *LSPDiagnosticsTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	server := GetStringArg(args, "server", "gopls")
	server = strings.TrimSpace(server)
	if server == "" {
		return toolErrorf("server is required"), nil
	}

	timeoutMS := GetIntArg(args, "timeout_ms", 4000)
	if timeoutMS <= 0 {
		timeoutMS = 4000
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond

	languageID := strings.TrimSpace(GetStringArg(args, "language_id", ""))

	rawFiles, ok := args["files"]
	if !ok {
		return toolErrorf("files is required"), nil
	}
	filesAny, ok := rawFiles.([]interface{})
	if !ok || len(filesAny) == 0 {
		return toolErrorf("files must be a non-empty array"), nil
	}

	files := make([]string, 0, len(filesAny))
	for _, f := range filesAny {
		s, ok := f.(string)
		if !ok {
			return toolErrorf("files must contain only strings"), nil
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		files = append(files, s)
	}
	if len(files) == 0 {
		return toolErrorf("files must be a non-empty array"), nil
	}

	serverArgs := []string{}
	if rawArgs, ok := args["args"]; ok {
		if arr, ok := rawArgs.([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						serverArgs = append(serverArgs, s)
					}
				}
			}
		}
	}

	rootAbs, err := filepath.Abs(t.workspaceRoot)
	if err != nil {
		return toolError(err), nil
	}
	rootAbs = filepath.Clean(rootAbs)

	type openFile struct {
		Rel        string
		Abs        string
		URI        string
		LanguageID string
		Text       string
	}

	opened := make([]openFile, 0, len(files))
	uriToRel := make(map[string]string, len(files))

	for _, rel := range files {
		abs, err := resolvePathInWorkspace(t.workspaceRoot, rel)
		if err != nil {
			return toolError(err), nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return toolError(err), nil
		}
		if len(data) > 1024*1024 {
			return toolErrorf("file too large for diagnostics: %s", rel), nil
		}
		uri := pathToFileURI(abs)
		lang := languageID
		if lang == "" {
			lang = inferLanguageID(abs)
		}
		opened = append(opened, openFile{
			Rel:        rel,
			Abs:        abs,
			URI:        uri,
			LanguageID: lang,
			Text:       string(data),
		})
		uriToRel[uri] = rel
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := startLSPClient(ctx, server, serverArgs)
	if err != nil {
		return toolError(err), nil
	}
	defer client.Close()

	if err := client.Initialize(ctx, rootAbs); err != nil {
		return toolError(err), nil
	}

	for _, f := range opened {
		if err := client.DidOpen(ctx, f.URI, f.LanguageID, f.Text); err != nil {
			return toolError(err), nil
		}
	}

	diags := client.CollectDiagnostics(ctx, keys(uriToRel))

	report := buildDiagnosticsReport(rootAbs, server, serverArgs, uriToRel, diags)
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return toolError(err), nil
	}
	return successResult(string(out), report), nil
}

func (t *LSPDiagnosticsTool) Preview(ctx context.Context, args api.Args) (*api.Preview, error) {
	server := GetStringArg(args, "server", "gopls")
	timeoutMS := GetIntArg(args, "timeout_ms", 4000)

	rawFiles, _ := args["files"].([]interface{})
	count := 0
	for _, f := range rawFiles {
		if _, ok := f.(string); ok {
			count++
		}
	}

	return &api.Preview{
		Kind:     api.PreviewCommand,
		Summary:  "Run LSP diagnostics: " + server,
		Content:  server + " (stdio)",
		Affected: []string{t.workspaceRoot},
		RiskHint: fmt.Sprintf("Files: %d, timeout: %dms", count, timeoutMS),
	}, nil
}

type diagnosticsReport struct {
	Server struct {
		Command string   `json:"command"`
		Args    []string `json:"args,omitempty"`
	} `json:"server"`
	Root    string            `json:"root"`
	Files   []fileDiagnostics `json:"files"`
	Summary struct {
		FilesWithDiagnostics int `json:"files_with_diagnostics"`
		TotalDiagnostics     int `json:"total_diagnostics"`
	} `json:"summary"`
}

type fileDiagnostics struct {
	File  string          `json:"file"`
	URI   string          `json:"uri"`
	Items []diagnosticOut `json:"items,omitempty"`
}

type diagnosticOut struct {
	Severity  string `json:"severity,omitempty"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
	Code      any    `json:"code,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

func buildDiagnosticsReport(rootAbs, server string, serverArgs []string, uriToRel map[string]string, diags map[string][]lspDiagnostic) diagnosticsReport {
	var report diagnosticsReport
	report.Server.Command = server
	report.Server.Args = append([]string(nil), serverArgs...)
	report.Root = rootAbs

	uris := make([]string, 0, len(uriToRel))
	for uri := range uriToRel {
		uris = append(uris, uri)
	}
	sortStrings(uris)

	total := 0
	with := 0

	for _, uri := range uris {
		rel := uriToRel[uri]
		fd := fileDiagnostics{File: rel, URI: uri}
		items := diags[uri]
		if len(items) > 0 {
			with++
		}
		for _, d := range items {
			fd.Items = append(fd.Items, diagnosticOut{
				Severity:  severityString(d.Severity),
				Message:   d.Message,
				Source:    d.Source,
				Code:      d.Code,
				Line:      d.Range.Start.Line + 1,
				Column:    d.Range.Start.Character + 1,
				EndLine:   d.Range.End.Line + 1,
				EndColumn: d.Range.End.Character + 1,
			})
			total++
		}
		report.Files = append(report.Files, fd)
	}

	report.Summary.FilesWithDiagnostics = with
	report.Summary.TotalDiagnostics = total
	return report
}

func severityString(n int) string {
	switch n {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return ""
	}
}

func inferLanguageID(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

func pathToFileURI(absPath string) string {
	absPath = filepath.Clean(absPath)
	if filepath.Separator != '/' {
		absPath = strings.ReplaceAll(absPath, string(filepath.Separator), "/")
	}
	if !strings.HasPrefix(absPath, "/") {
		return "file:///" + absPath
	}
	return "file://" + absPath
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

type lspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage

	diagMu sync.Mutex
	diags  map[string][]lspDiagnostic
}

func startLSPClient(ctx context.Context, server string, serverArgs []string) (*lspClient, error) {
	cmd := exec.CommandContext(ctx, server, serverArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &lspClient{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		nextID:  1,
		pending: make(map[int]chan json.RawMessage),
		diags:   make(map[string][]lspDiagnostic),
	}

	go c.readLoop()
	return c, nil
}

func (c *lspClient) Close() {
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func (c *lspClient) Initialize(ctx context.Context, rootPath string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(rootPath),
		"capabilities": map[string]any{
			"textDocument": map[string]any{},
			"workspace":    map[string]any{},
		},
		"clientInfo": map[string]any{
			"name":    "agent-engine",
			"version": "0.1",
		},
	}

	var result any
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *lspClient) DidOpen(ctx context.Context, uri, languageID, text string) error {
	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    1,
			"text":       text,
		},
	}
	return c.notify("textDocument/didOpen", params)
}

func (c *lspClient) CollectDiagnostics(ctx context.Context, expectedURIs []string) map[string][]lspDiagnostic {
	expect := make(map[string]bool, len(expectedURIs))
	for _, u := range expectedURIs {
		expect[u] = true
	}
	received := make(map[string]bool, len(expectedURIs))

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return c.snapshotDiagnostics(expectedURIs)
		case <-tick.C:
			c.diagMu.Lock()
			for uri := range expect {
				if _, ok := c.diags[uri]; ok {
					received[uri] = true
				}
			}
			c.diagMu.Unlock()

			all := true
			for uri := range expect {
				if !received[uri] {
					all = false
					break
				}
			}
			if all {
				return c.snapshotDiagnostics(expectedURIs)
			}
		}
	}
}

func (c *lspClient) snapshotDiagnostics(expectedURIs []string) map[string][]lspDiagnostic {
	out := make(map[string][]lspDiagnostic, len(expectedURIs))
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	for _, uri := range expectedURIs {
		out[uri] = append([]lspDiagnostic(nil), c.diags[uri]...)
	}
	return out
}

func (c *lspClient) request(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := lspRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(req); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case raw := <-ch:
		if out == nil {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
}

func (c *lspClient) notify(method string, params any) error {
	n := lspNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(n)
}

func (c *lspClient) writeMessage(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(data))
	buf.Write(data)

	_, err = c.stdin.Write(buf.Bytes())
	return err
}

func (c *lspClient) readLoop() {
	r := bufio.NewReader(c.stdout)
	for {
		msg, err := readLSPMessage(r)
		if err != nil {
			return
		}
		c.handleIncoming(msg)
	}
}

func (c *lspClient) handleIncoming(msg []byte) {
	var envelope lspEnvelope
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return
	}

	if envelope.Method == "textDocument/publishDiagnostics" {
		var params publishDiagnosticsParams
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			return
		}
		c.diagMu.Lock()
		c.diags[params.URI] = append([]lspDiagnostic(nil), params.Diagnostics...)
		c.diagMu.Unlock()
		return
	}

	if envelope.ID != 0 {
		c.mu.Lock()
		ch := c.pending[envelope.ID]
		delete(c.pending, envelope.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- envelope.Result
		}
	}
}

func readLSPMessage(r *bufio.Reader) ([]byte, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			v := strings.TrimSpace(line[len("content-length:"):])
			n, err := strconv.Atoi(v)
			if err == nil {
				contentLength = n
			}
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("invalid content-length")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type lspRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type lspNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type lspEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type publishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity,omitempty"`
	Code     any      `json:"code,omitempty"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
