package tools

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// GlobTool finds files matching a pattern
type GlobTool struct {
	BaseTool
	workspaceRoot string
	maxResults    int
}

// NewGlobTool creates a new glob tool
func NewGlobTool(workspaceRoot string) *GlobTool {
	return &GlobTool{
		BaseTool: NewBaseTool(
			"glob",
			"Find files matching a glob pattern (e.g., '**/*.go', 'src/*.js'). Returns matching file paths.",
			[]ParameterDef{
				{Name: "pattern", Type: "string", Description: "Glob pattern to match (e.g., **/*.go, src/**/*.ts)", Required: true},
				{Name: "path", Type: "string", Description: "Base directory to search from (default: workspace root)", Required: false},
			},
			api.RiskNone,
		),
		workspaceRoot: workspaceRoot,
		maxResults:    100, // Limit results
	}
}

func (t *GlobTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	pattern := GetStringArg(args, "pattern", "")
	if pattern == "" {
		return toolErrorf("pattern is required"), nil
	}

	basePath := GetStringArg(args, "path", ".")

	// Resolve base path
	absBase, err := resolvePathInWorkspace(t.workspaceRoot, basePath)
	if err != nil {
		return toolError(err), nil
	}
	rootAbs, _ := filepath.Abs(t.workspaceRoot)

	// Handle glob pattern
	var matches []string

	if strings.Contains(pattern, "**") {
		// Recursive glob - walk the tree
		matches, err = t.recursiveGlob(absBase, pattern)
	} else {
		// Simple glob
		fullPattern := filepath.Join(absBase, pattern)
		matches, err = filepath.Glob(fullPattern)
	}

	if err != nil {
		return toolError(err), nil
	}

	// Convert to relative paths and sort
	var relativePaths []string
	for _, match := range matches {
		rel, err := filepath.Rel(rootAbs, match)
		if err != nil {
			rel = match
		}
		relativePaths = append(relativePaths, rel)
	}
	sort.Strings(relativePaths)

	// Limit results
	if len(relativePaths) > t.maxResults {
		truncated := relativePaths[:t.maxResults]
		return successText(strings.Join(truncated, "\n") +
			"\n\n... (truncated, showing first " + strconv.Itoa(t.maxResults) + " results)"), nil
	}

	if len(relativePaths) == 0 {
		return successText("No files found matching pattern: " + pattern), nil
	}

	return successText(strings.Join(relativePaths, "\n")), nil
}

func (t *GlobTool) recursiveGlob(basePath, pattern string) ([]string, error) {
	var matches []string

	// Split pattern into prefix and suffix around **
	parts := strings.SplitN(pattern, "**", 2)
	prefix := parts[0]
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimPrefix(parts[1], "/")
		suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
	}

	// Walk the directory tree
	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip hidden directories
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
			return filepath.SkipDir
		}

		// Skip directories for matching
		if info.IsDir() {
			return nil
		}

		// Get relative path from base
		relPath, err := filepath.Rel(basePath, path)
		if err != nil {
			return nil
		}

		// Check prefix
		if prefix != "" && !strings.HasPrefix(relPath, strings.TrimSuffix(prefix, "/")) {
			return nil
		}

		// Check suffix (file extension pattern)
		if suffix != "" {
			matched, _ := filepath.Match(suffix, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		matches = append(matches, path)

		// Limit matches to prevent runaway
		if len(matches) > t.maxResults*2 {
			return filepath.SkipAll
		}

		return nil
	})

	return matches, err
}
