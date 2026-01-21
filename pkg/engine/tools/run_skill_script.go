package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"AgentEngine/pkg/engine/api"
)

// Error sentinel values for run_skill_script validation.
var (
	ErrNoActiveSkill  = errors.New("no active skill")
	ErrAbsolutePath   = errors.New("absolute path not allowed")
	ErrPathTraversal  = errors.New("path traversal (.. ) not allowed")
	ErrSymlinkEscape  = errors.New("symlink escapes skill directory")
	ErrScriptNotFound = errors.New("script not found")
)

// RunSkillScriptTool executes scripts within the active skill's scripts/ directory.
// This is a controlled alternative to the general-purpose shell tool.
type RunSkillScriptTool struct {
	BaseTool
	workspaceRoot string
	skillIndex    SkillIndexLookup
	timeout       time.Duration
	maxOutput     int
}

// SkillIndexLookup is the minimal interface needed to resolve skill paths.
type SkillIndexLookup interface {
	Get(name string) (api.SkillMeta, bool)
}

// NewRunSkillScriptTool creates a new run_skill_script tool.
func NewRunSkillScriptTool(workspaceRoot string, skillIndex SkillIndexLookup) *RunSkillScriptTool {
	return &RunSkillScriptTool{
		BaseTool: NewBaseTool(
			"run_skill_script",
			"Execute a script from the active skill's scripts/ directory. "+
				"More secure than shell: only pre-defined scripts can run.",
			[]ParameterDef{
				{Name: "script", Type: "string", Description: "Script path relative to scripts/ (e.g. build.sh)", Required: true},
				{Name: "args", Type: "array", Description: "Arguments to pass to the script", Required: false},
				{Name: "timeout_sec", Type: "integer", Description: "Timeout in seconds (default: 60, max: 300)", Required: false},
			},
			api.RiskHigh,
		),
		workspaceRoot: workspaceRoot,
		skillIndex:    skillIndex,
		timeout:       60 * time.Second,
		maxOutput:     100 * 1024, // 100KB
	}
}

// ValidateScriptPath ensures the script path is safe and within the skill's scripts/ directory.
func (t *RunSkillScriptTool) ValidateScriptPath(skillPath, script string) (string, error) {
	// 1. Reject absolute paths
	if filepath.IsAbs(script) {
		return "", ErrAbsolutePath
	}

	// 2. Reject path traversal
	cleanScript := filepath.Clean(script)
	if strings.Contains(cleanScript, "..") {
		return "", ErrPathTraversal
	}

	// 3. Build full path
	scriptsDir := filepath.Join(skillPath, "scripts")
	fullPath := filepath.Join(scriptsDir, cleanScript)

	// 4. Canonicalize and check boundary
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	absScriptsDir, err := filepath.Abs(scriptsDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve scripts dir: %w", err)
	}

	if !strings.HasPrefix(absPath, absScriptsDir+string(filepath.Separator)) && absPath != absScriptsDir {
		return "", fmt.Errorf("%s: path escapes scripts directory", api.ErrWorkspaceEscape)
	}

	// 5. Check symlink escape
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrScriptNotFound
		}
		return "", fmt.Errorf("failed to evaluate symlinks: %w", err)
	}

	realScriptsDir, _ := filepath.EvalSymlinks(absScriptsDir)
	if realScriptsDir == "" {
		realScriptsDir = absScriptsDir
	}

	if !strings.HasPrefix(realPath, realScriptsDir+string(filepath.Separator)) && realPath != realScriptsDir {
		return "", ErrSymlinkEscape
	}

	// 6. Check file exists and is executable
	info, err := os.Stat(realPath)
	if err != nil {
		return "", ErrScriptNotFound
	}
	if info.IsDir() {
		return "", fmt.Errorf("script path is a directory, not a file")
	}

	return absPath, nil
}

func (t *RunSkillScriptTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	// Get active skill from context metadata
	activeSkill := GetStringArg(args, "_active_skill", "")
	if activeSkill == "" {
		return toolErrorf("%s: no active skill", api.ErrPolicyDenied), nil
	}

	// Resolve skill path
	meta, ok := t.skillIndex.Get(activeSkill)
	if !ok {
		return toolErrorf("skill not found: %s", activeSkill), nil
	}

	// Get script parameter
	script := GetStringArg(args, "script", "")
	if script == "" {
		return toolErrorf("script is required"), nil
	}

	// Validate script path
	scriptPath, err := t.ValidateScriptPath(meta.Path, script)
	if err != nil {
		return toolErrorf("script validation failed: %v", err), nil
	}

	// Get args
	var scriptArgs []string
	if rawArgs, ok := args["args"]; ok {
		switch v := rawArgs.(type) {
		case []string:
			scriptArgs = v
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					scriptArgs = append(scriptArgs, s)
				}
			}
		}
	}

	// Get timeout
	timeoutSecs := GetIntArg(args, "timeout_sec", 60)
	if timeoutSecs > 300 {
		timeoutSecs = 300
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	// Execute
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := append([]string{scriptPath}, scriptArgs...)
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = meta.Path

	// Set environment variables
	cmd.Env = append(os.Environ(),
		"WORKSPACE_ROOT="+t.workspaceRoot,
		"SKILL_PATH="+meta.Path,
		"SKILL_NAME="+meta.Name,
	)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Build output
	var output strings.Builder
	if stdout.Len() > 0 {
		stdoutStr := stdout.String()
		if len(stdoutStr) > t.maxOutput {
			stdoutStr = stdoutStr[:t.maxOutput] + "\n\n... (stdout truncated)"
		}
		output.WriteString(stdoutStr)
	}
	if stderr.Len() > 0 {
		stderrStr := stderr.String()
		if len(stderrStr) > t.maxOutput/2 {
			stderrStr = stderrStr[:t.maxOutput/2] + "\n\n... (stderr truncated)"
		}
		lines := strings.Split(strings.TrimSpace(stderrStr), "\n")
		for _, line := range lines {
			output.WriteString("[stderr] " + line + "\n")
		}
	}

	// Handle errors
	if ctx.Err() == context.DeadlineExceeded {
		return api.ToolResult{
			Content: output.String() + fmt.Sprintf("\n\nError: Script timed out after %d seconds", timeoutSecs),
			Status:  "error",
			Error:   "timeout",
			Data:    map[string]any{"exit_code": -1},
		}, nil
	}

	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return api.ToolResult{
			Content: output.String() + fmt.Sprintf("\n\nExit code: %d", exitCode),
			Status:  "error",
			Error:   fmt.Sprintf("exit code %d", exitCode),
			Data:    map[string]any{"exit_code": exitCode},
		}, nil
	}

	// Success
	if output.Len() == 0 {
		return successText("<script completed with no output>"), nil
	}
	return successText(output.String()), nil
}

func (t *RunSkillScriptTool) Preview(ctx context.Context, args api.Args) (*api.Preview, error) {
	activeSkill := GetStringArg(args, "_active_skill", "")
	script := GetStringArg(args, "script", "")

	var scriptArgs []string
	if rawArgs, ok := args["args"]; ok {
		if v, ok := rawArgs.([]any); ok {
			for _, a := range v {
				if s, ok := a.(string); ok {
					scriptArgs = append(scriptArgs, s)
				}
			}
		}
	}

	cmdLine := script
	if len(scriptArgs) > 0 {
		cmdLine = script + " " + strings.Join(scriptArgs, " ")
	}

	var affected []string
	if activeSkill != "" {
		if meta, ok := t.skillIndex.Get(activeSkill); ok {
			affected = []string{filepath.Join(meta.Path, "scripts", script)}
		}
	}

	return &api.Preview{
		Kind:     api.PreviewCommand,
		Summary:  fmt.Sprintf("Execute skill script: %s", script),
		Content:  cmdLine,
		Affected: affected,
		RiskHint: "Script runs with current user permissions. May modify filesystem.",
	}, nil
}
