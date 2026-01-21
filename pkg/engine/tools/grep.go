package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// GrepTool searches for text patterns in files
type GrepTool struct {
	BaseTool
	workspaceRoot string
	maxResults    int
	maxFileSize   int64
}

// NewGrepTool creates a new grep tool
func NewGrepTool(workspaceRoot string) *GrepTool {
	return &GrepTool{
		BaseTool: NewBaseTool(
			"grep",
			"Search for text patterns in files. Returns matching lines with file paths and line numbers.",
			[]ParameterDef{
				{Name: "pattern", Type: "string", Description: "Text or regex pattern to search for", Required: true},
				{Name: "path", Type: "string", Description: "File or directory to search in (default: workspace root)", Required: false},
				{Name: "include", Type: "string", Description: "File glob pattern to include (e.g., *.go, *.js)", Required: false},
				{Name: "ignore_case", Type: "boolean", Description: "Case-insensitive search", Required: false},
			},
			api.RiskNone,
		),
		workspaceRoot: workspaceRoot,
		maxResults:    50,
		maxFileSize:   1024 * 1024, // 1MB
	}
}

// GrepMatch represents a single search match
type GrepMatch struct {
	File    string
	Line    int
	Content string
}

func (t *GrepTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	pattern := GetStringArg(args, "pattern", "")
	if pattern == "" {
		return toolErrorf("pattern is required"), nil
	}

	searchPath := GetStringArg(args, "path", ".")
	include := GetStringArg(args, "include", "")
	ignoreCase := GetBoolArg(args, "ignore_case", false)

	// Resolve path
	absPath, err := resolvePathInWorkspace(t.workspaceRoot, searchPath)
	if err != nil {
		return toolError(err), nil
	}
	rootAbs, _ := filepath.Abs(t.workspaceRoot)

	// Build regex
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Fall back to literal search
		re = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}

	// Collect files to search
	var files []string
	info, err := os.Stat(absPath)
	if err != nil {
		return toolErrorf("path not found: %s", searchPath), nil
	}

	if info.IsDir() {
		files, err = t.collectFiles(absPath, include)
		if err != nil {
			return toolError(err), nil
		}
	} else {
		files = []string{absPath}
	}

	// Search files
	var matches []GrepMatch
	for _, file := range files {
		if len(matches) >= t.maxResults {
			break
		}

		fileMatches, err := t.searchFile(file, re)
		if err != nil {
			continue // Skip files we can't read
		}

		matches = append(matches, fileMatches...)
	}

	// Format output
	if len(matches) == 0 {
		return successText("No matches found for pattern: " + pattern), nil
	}

	var output strings.Builder
	for i, m := range matches {
		if i >= t.maxResults {
			output.WriteString(fmt.Sprintf("\n... (showing first %d matches)", t.maxResults))
			break
		}

		rel, _ := filepath.Rel(rootAbs, m.File)
		output.WriteString(fmt.Sprintf("%s:%d: %s\n", rel, m.Line, strings.TrimSpace(m.Content)))
	}

	return successText(output.String()), nil
}

func (t *GrepTool) collectFiles(dir, include string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden directories
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			// Skip common non-text directories
			if name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip large files
		if info.Size() > t.maxFileSize {
			return nil
		}

		// Apply include filter
		if include != "" {
			matched, _ := filepath.Match(include, info.Name())
			if !matched {
				return nil
			}
		}

		// Skip binary files (basic check)
		if t.isBinaryFile(path) {
			return nil
		}

		files = append(files, path)
		return nil
	})

	return files, err
}

func (t *GrepTool) searchFile(path string, re *regexp.Regexp) ([]GrepMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []GrepMatch
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if re.MatchString(line) {
			matches = append(matches, GrepMatch{
				File:    path,
				Line:    lineNum,
				Content: line,
			})

			// Limit matches per file
			if len(matches) >= 10 {
				break
			}
		}
	}

	return matches, scanner.Err()
}

func (t *GrepTool) isBinaryFile(path string) bool {
	// Check by extension
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := map[string]bool{
		".exe": true, ".bin": true, ".so": true, ".dylib": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	}
	return binaryExts[ext]
}
