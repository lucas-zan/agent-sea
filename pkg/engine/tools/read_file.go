package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// ReadFileTool reads file contents
type ReadFileTool struct {
	BaseTool
	workspaceRoot string
	maxBytes      int64 // Maximum bytes to read
}

// NewReadFileTool creates a new read_file tool
func NewReadFileTool(workspaceRoot string) *ReadFileTool {
	return &ReadFileTool{
		BaseTool: NewBaseTool(
			"read_file",
			"Read the contents of a file. Returns the file content as text. For large files, content may be truncated.",
			[]ParameterDef{
				{Name: "path", Type: "string", Description: "Path to the file to read (relative to workspace)", Required: true},
				{Name: "start_line", Type: "integer", Description: "Start line number (1-indexed, optional)", Required: false},
				{Name: "end_line", Type: "integer", Description: "End line number (1-indexed, inclusive, optional)", Required: false},
			},
			api.RiskNone,
		),
		workspaceRoot: workspaceRoot,
		maxBytes:      500 * 1024, // 500KB default
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	path := GetStringArg(args, "path", "")
	if path == "" {
		return toolErrorf("path is required"), nil
	}

	startLine := GetIntArg(args, "start_line", 0)
	endLine := GetIntArg(args, "end_line", 0)

	// Resolve path
	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	if err != nil {
		return toolError(err), nil
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErrorf("file does not exist: %s", path), nil
		}
		return toolError(err), nil
	}

	if info.IsDir() {
		return toolErrorf("path is a directory, not a file: %s", path), nil
	}

	// Check file size
	if info.Size() > t.maxBytes && startLine == 0 && endLine == 0 {
		return toolErrorf("file is too large (%s). Use start_line and end_line to read specific portions.",
			formatSize(info.Size())), nil
	}

	// Read file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return toolError(err), nil
	}

	// Handle line range if specified
	if startLine > 0 || endLine > 0 {
		lines := strings.Split(string(content), "\n")

		if startLine < 1 {
			startLine = 1
		}
		if endLine < startLine {
			endLine = len(lines)
		}
		if startLine > len(lines) {
			return toolErrorf("start_line (%d) exceeds file length (%d lines)", startLine, len(lines)), nil
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}

		// Extract lines (1-indexed to 0-indexed)
		selectedLines := lines[startLine-1 : endLine]

		// Add line numbers
		var result strings.Builder
		for i, line := range selectedLines {
			lineNum := startLine + i
			result.WriteString(fmt.Sprintf("%4d: %s\n", lineNum, line))
		}

		return successText(result.String()), nil
	}

	// Return full content with truncation warning if needed
	contentStr := string(content)
	if int64(len(content)) > t.maxBytes {
		contentStr = contentStr[:t.maxBytes] + "\n\n... (content truncated)"
	}

	return successText(contentStr), nil
}
