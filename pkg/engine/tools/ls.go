package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// LsTool lists directory contents
type LsTool struct {
	BaseTool
	workspaceRoot string
}

// NewLsTool creates a new ls tool
func NewLsTool(workspaceRoot string) *LsTool {
	return &LsTool{
		BaseTool: NewBaseTool(
			"ls",
			"List files and directories in a given path. Returns file names, types, and sizes.",
			[]ParameterDef{
				{Name: "path", Type: "string", Description: "Directory path to list (relative to workspace)", Required: true},
				{Name: "all", Type: "boolean", Description: "Include hidden files (starting with .)", Required: false},
			},
			api.RiskNone,
		),
		workspaceRoot: workspaceRoot,
	}
}

func (t *LsTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	path := GetStringArg(args, "path", ".")
	showAll := GetBoolArg(args, "all", false)

	// Resolve path
	absPath, err := resolvePathInWorkspace(t.workspaceRoot, path)
	if err != nil {
		return toolError(err), nil
	}

	// Check if path exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErrorf("path does not exist: %s", path), nil
		}
		return toolError(err), nil
	}

	// If it's a file, just show info about that file
	if !info.IsDir() {
		return successText(formatFileInfo(path, info)), nil
	}

	// Read directory
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return toolError(err), nil
	}

	// Build output
	var lines []string
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files unless --all
		if !showAll && strings.HasPrefix(name, ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s (error: %v)", name, err))
			continue
		}

		lines = append(lines, formatFileInfo(name, info))
	}

	// Sort entries
	sort.Strings(lines)

	if len(lines) == 0 {
		return successText("(empty directory)"), nil
	}

	return successText(strings.Join(lines, "\n")), nil
}

func formatFileInfo(name string, info os.FileInfo) string {
	if info.IsDir() {
		return fmt.Sprintf("📁 %s/", name)
	}

	size := formatSize(info.Size())
	return fmt.Sprintf("📄 %s (%s)", name, size)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
