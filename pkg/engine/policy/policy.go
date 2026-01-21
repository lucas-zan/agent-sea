// Package policy provides unified tool governance for the agent engine.
package policy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Policy Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Tool is the minimal interface needed for policy decisions.
type Tool interface {
	Name() string
}

// ToolWithMeta extends Tool with metadata for policy decisions.
type ToolWithMeta interface {
	Tool
	Risk() api.RiskLevel
}

// Policy defines the unified interface for tool governance.
type Policy interface {
	// Filter returns the subset of tools visible to the LLM.
	Filter(ctx context.Context, pctx api.PolicyContext, tools []Tool) []Tool

	// NeedApproval returns true if the tool call requires user approval.
	NeedApproval(ctx context.Context, pctx api.PolicyContext, tool Tool, args api.Args) bool

	// Validate checks if the tool call is allowed. Returns error if denied.
	Validate(ctx context.Context, pctx api.PolicyContext, tool Tool, args api.Args) error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DefaultPolicy
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DefaultPolicy implements the standard policy rules.
type DefaultPolicy struct {
	// DangerousCommands patterns that require approval even in auto mode
	DangerousCommands []string
}

// NewDefaultPolicy creates a new default policy.
func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{
		DangerousCommands: []string{
			"rm ", "rm\t", "rmdir",
			"sudo ", "chmod ", "chown ",
			"mv ", "cp -r",
			"> ", ">>",
			"curl ", "wget ",
			"git push", "git reset --hard",
		},
	}
}

// Filter returns tools visible to the LLM based on policy context.
func (p *DefaultPolicy) Filter(ctx context.Context, pctx api.PolicyContext, tools []Tool) []Tool {
	// If no skill-level restrictions, return all tools
	if len(pctx.AllowedTools) == 0 {
		return tools
	}

	// Build allowlist map
	allowedMap := make(map[string]bool)
	for _, name := range pctx.AllowedTools {
		allowedMap[name] = true
	}

	// Filter: include if in allowlist OR is a system tool
	var filtered []Tool
	for _, t := range tools {
		if allowedMap[t.Name()] || api.IsSystemTool(t.Name()) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// NeedApproval determines if a tool call requires user approval.
func (p *DefaultPolicy) NeedApproval(ctx context.Context, pctx api.PolicyContext, tool Tool, args api.Args) bool {
	switch pctx.ApprovalMode {
	case api.ModeSuggest:
		// All tools need approval in suggest mode
		return true

	case api.ModeFullAuto:
		// No approval needed in full-auto mode
		return false

	case api.ModeAuto:
		fallthrough
	default:
		// Auto mode: check tool risk and operation type
		return p.needApprovalAuto(tool, args)
	}
}

// needApprovalAuto implements approval logic for ModeAuto.
func (p *DefaultPolicy) needApprovalAuto(tool Tool, args api.Args) bool {
	toolName := tool.Name()

	// System tools that write data need approval
	if toolName == "write_todos" || toolName == "update_memory" {
		return true
	}

	// Check tool risk level
	if tm, ok := tool.(ToolWithMeta); ok {
		if tm.Risk() == api.RiskHigh {
			return true
		}
	}

	// Check for dangerous patterns in shell commands
	if toolName == "shell" || toolName == "run_command" {
		if command, ok := args["command"].(string); ok {
			for _, pattern := range p.DangerousCommands {
				if strings.Contains(command, pattern) {
					return true
				}
			}
		}
	}

	// Write operations typically need approval
	highRiskTools := map[string]bool{
		"write_file":       true,
		"edit_file":        true,
		"delete_file":      true,
		"shell":            true,
		"run_command":      true,
		"run_skill_script": true,
	}
	return highRiskTools[toolName]
}

// Validate checks if a tool call is allowed.
func (p *DefaultPolicy) Validate(ctx context.Context, pctx api.PolicyContext, tool Tool, args api.Args) error {
	toolName := tool.Name()

	// Check allowed-tools constraint (skip for system tools)
	if len(pctx.AllowedTools) > 0 && !api.IsSystemTool(toolName) {
		allowed := false
		for _, name := range pctx.AllowedTools {
			if name == toolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return &PolicyError{
				Code:    api.ErrPolicyDenied,
				Message: fmt.Sprintf("tool %q not in skill allowed-tools", toolName),
			}
		}
	}

	// Check workspace boundary for file operations
	if path, ok := args["path"].(string); ok && pctx.WorkspaceRoot != "" {
		if err := p.validatePath(path, pctx.WorkspaceRoot); err != nil {
			return err
		}
	}

	return nil
}

// validatePath ensures a path is within the workspace boundary.
func (p *DefaultPolicy) validatePath(targetPath, workspaceRoot string) error {
	// Handle relative paths
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspaceRoot, targetPath)
	}

	// Resolve to absolute canonical path
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return &PolicyError{
			Code:    api.ErrWorkspaceEscape,
			Message: fmt.Sprintf("invalid path: %v", err),
		}
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return &PolicyError{
			Code:    api.ErrWorkspaceEscape,
			Message: fmt.Sprintf("invalid workspace root: %v", err),
		}
	}

	// Check if path is within workspace
	if !strings.HasPrefix(absPath, absWorkspace+string(filepath.Separator)) && absPath != absWorkspace {
		return &PolicyError{
			Code:    api.ErrWorkspaceEscape,
			Message: fmt.Sprintf("path %q escapes workspace boundary", targetPath),
		}
	}

	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PolicyError
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PolicyError represents a policy violation.
type PolicyError struct {
	Code    string
	Message string
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
