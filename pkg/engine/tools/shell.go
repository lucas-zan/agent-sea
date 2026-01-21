package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"AgentEngine/pkg/engine/api"
)

// ShellTool executes shell commands
type ShellTool struct {
	BaseTool
	workspaceRoot  string
	timeout        time.Duration
	maxOutputBytes int
}

// NewShellTool creates a new shell tool
func NewShellTool(workspaceRoot string) *ShellTool {
	return &ShellTool{
		BaseTool: NewBaseTool(
			"shell",
			"Execute a shell command in the workspace. Use for running build commands, tests, git operations, or any CLI tools.",
			[]ParameterDef{
				{Name: "command", Type: "string", Description: "Shell command to execute", Required: true},
				{Name: "timeout", Type: "integer", Description: "Timeout in seconds (default: 120)", Required: false},
			},
			api.RiskHigh,
		),
		workspaceRoot:  workspaceRoot,
		timeout:        120 * time.Second,
		maxOutputBytes: 100 * 1024, // 100KB
	}
}

func (t *ShellTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	command := GetStringArg(args, "command", "")
	if command == "" {
		return toolErrorf("command is required"), nil
	}

	timeoutSecs := GetIntArg(args, "timeout", 120)
	timeout := time.Duration(timeoutSecs) * time.Second
	if timeout > 300*time.Second {
		timeout = 300 * time.Second // Max 5 minutes
	}

	// Create command with context timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = t.workspaceRoot

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()

	// Build output
	var output strings.Builder

	// Add stdout
	if stdout.Len() > 0 {
		stdoutStr := stdout.String()
		if len(stdoutStr) > t.maxOutputBytes {
			stdoutStr = stdoutStr[:t.maxOutputBytes] + "\n\n... (stdout truncated)"
		}
		output.WriteString(stdoutStr)
	}

	// Add stderr
	if stderr.Len() > 0 {
		stderrStr := stderr.String()
		if len(stderrStr) > t.maxOutputBytes/2 {
			stderrStr = stderrStr[:t.maxOutputBytes/2] + "\n\n... (stderr truncated)"
		}

		// Add stderr with prefix
		lines := strings.Split(strings.TrimSpace(stderrStr), "\n")
		for _, line := range lines {
			output.WriteString("[stderr] " + line + "\n")
		}
	}

	// Handle errors
	if ctx.Err() == context.DeadlineExceeded {
		return api.ToolResult{
			Content: output.String() + fmt.Sprintf("\n\nError: Command timed out after %d seconds", timeoutSecs),
			Status:  "error",
			Error:   "timeout",
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
		}, nil
	}

	// Success
	if output.Len() == 0 {
		return successText("<command completed with no output>"), nil
	}

	return successText(output.String()), nil
}

func (t *ShellTool) Preview(ctx context.Context, args api.Args) (*api.Preview, error) {
	command := GetStringArg(args, "command", "")
	timeoutSecs := GetIntArg(args, "timeout", 120)

	return &api.Preview{
		Kind:     api.PreviewCommand,
		Summary:  "Execute shell command",
		Content:  command,
		Affected: []string{t.workspaceRoot},
		RiskHint: fmt.Sprintf("Timeout: %d seconds", timeoutSecs),
	}, nil
}
