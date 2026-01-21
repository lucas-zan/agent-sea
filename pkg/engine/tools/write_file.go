package tools

import (
	"context"
	"os"
	"path/filepath"

	"AgentEngine/pkg/engine/api"
)

// WriteFileTool creates or overwrites files
type WriteFileTool struct {
	BaseTool
	workspaceRoot string
}

// NewWriteFileTool creates a new write_file tool
func NewWriteFileTool(workspaceRoot string) *WriteFileTool {
	return &WriteFileTool{
		BaseTool: NewBaseTool(
			"write_file",
			"Create a new file or overwrite an existing file with the specified content. Creates parent directories if needed.",
			[]ParameterDef{
				{Name: "path", Type: "string", Description: "Path to the file to create/overwrite (relative to workspace)", Required: true},
				{Name: "content", Type: "string", Description: "Content to write to the file", Required: true},
			},
			api.RiskHigh,
		),
		workspaceRoot: workspaceRoot,
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	path := GetStringArg(args, "path", "")
	if path == "" {
		return toolErrorf("path is required"), nil
	}

	content := GetStringArg(args, "content", "")

	// Resolve path
	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	if err != nil {
		return toolError(err), nil
	}

	// Check if file exists (for reporting)
	_, statErr := os.Stat(absPath)
	fileExists := statErr == nil

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return toolErrorf("failed to create directory %s: %v", dir, err), nil
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return toolError(err), nil
	}

	// Report
	if fileExists {
		return successText("✅ File overwritten: " + path), nil
	}
	return successText("✅ File created: " + path), nil
}

func (t *WriteFileTool) Preview(ctx context.Context, args api.Args) (*api.Preview, error) {
	path := GetStringArg(args, "path", "")
	content := GetStringArg(args, "content", "")

	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	if err != nil {
		absPath = "<invalid path: " + err.Error() + ">"
	}

	preview := content
	if len(preview) > 1000 {
		preview = preview[:1000] + "\n... (truncated)"
	}

	return &api.Preview{
		Kind:     api.PreviewDiff,
		Summary:  "Write file: " + path,
		Content:  preview,
		Affected: []string{absPath},
		RiskHint: "This operation modifies files on disk.",
	}, nil
}
