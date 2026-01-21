package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// EditFileTool makes targeted edits to existing files
type EditFileTool struct {
	BaseTool
	workspaceRoot string
}

// NewEditFileTool creates a new edit_file tool
func NewEditFileTool(workspaceRoot string) *EditFileTool {
	return &EditFileTool{
		BaseTool: NewBaseTool(
			"edit_file",
			"Make targeted edits to an existing file by replacing specific text. More precise than write_file for modifications.",
			[]ParameterDef{
				{Name: "path", Type: "string", Description: "Path to the file to edit (relative to workspace)", Required: true},
				{Name: "old_text", Type: "string", Description: "Exact text to find and replace (must match exactly)", Required: true},
				{Name: "new_text", Type: "string", Description: "Text to replace old_text with", Required: true},
			},
			api.RiskHigh,
		),
		workspaceRoot: workspaceRoot,
	}
}

func (t *EditFileTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	path := GetStringArg(args, "path", "")
	if path == "" {
		return toolErrorf("path is required"), nil
	}

	oldText := GetStringArg(args, "old_text", "")
	if oldText == "" {
		return toolErrorf("old_text is required"), nil
	}

	newText := GetStringArg(args, "new_text", "")

	// Resolve path
	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	if err != nil {
		return toolError(err), nil
	}

	// Read existing file
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErrorf("file does not exist: %s", path), nil
		}
		return toolError(err), nil
	}

	contentStr := string(content)

	// Check if old_text exists
	if !strings.Contains(contentStr, oldText) {
		return toolErrorf("old_text not found in file. Make sure it matches exactly including whitespace."), nil
	}

	// Count occurrences
	count := strings.Count(contentStr, oldText)
	if count > 1 {
		return toolErrorf("old_text found %d times in file. It must be unique. Provide more context.", count), nil
	}

	// Replace
	newContent := strings.Replace(contentStr, oldText, newText, 1)

	// Write back
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		return toolError(err), nil
	}

	return successText(fmt.Sprintf("✅ File edited: %s\nReplaced %d bytes with %d bytes", path, len(oldText), len(newText))), nil
}

func (t *EditFileTool) Preview(ctx context.Context, args api.Args) (*api.Preview, error) {
	path := GetStringArg(args, "path", "")
	oldText := GetStringArg(args, "old_text", "")
	newText := GetStringArg(args, "new_text", "")

	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	pathPreview := absPath
	if err != nil {
		pathPreview = "<invalid path: " + err.Error() + ">"
	}

	// Create unified diff-like preview
	var diffBuilder strings.Builder
	// Show old lines with - prefix
	oldLines := strings.Split(oldText, "\n")
	for _, line := range oldLines {
		diffBuilder.WriteString("- " + line + "\n")
	}

	// Show new lines with + prefix
	newLines := strings.Split(newText, "\n")
	for _, line := range newLines {
		diffBuilder.WriteString("+ " + line + "\n")
	}

	diffText := diffBuilder.String()
	if len(diffText) > 4000 {
		diffText = diffText[:4000] + "\n... (truncated)"
	}

	return &api.Preview{
		Kind:     api.PreviewDiff,
		Summary:  "Edit file: " + path,
		Content:  diffText,
		Affected: []string{pathPreview},
		RiskHint: fmt.Sprintf("Replacing %d bytes with %d bytes", len(oldText), len(newText)),
	}, nil
}
