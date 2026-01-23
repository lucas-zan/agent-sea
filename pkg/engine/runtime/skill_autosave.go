package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/tools"
)

func (r *TurnRunner) maybeAutoSaveSkillOutput(ctx context.Context, state *api.State, userMessage, assistantContent string) (loopOutcome, bool, error) {
	if r.session == nil || r.cfg.SkillIndex == nil {
		return loopOutcomeCompleted, false, nil
	}
	if strings.TrimSpace(r.session.ActiveSkill) == "" {
		return loopOutcomeCompleted, false, nil
	}
	if strings.TrimSpace(assistantContent) == "" {
		return loopOutcomeCompleted, false, nil
	}

	sk, err := r.cfg.SkillIndex.Load(r.session.ActiveSkill)
	if err != nil || sk == nil || sk.Metadata == nil {
		return loopOutcomeCompleted, false, nil
	}
	mode := strings.TrimSpace(strings.ToLower(sk.Metadata["autosave"]))
	if mode == "" {
		return loopOutcomeCompleted, false, nil
	}

	switch mode {
	case "novel_chapter":
		// Check if content looks like a summary (not actual chapter)
		if looksLikeSummary(assistantContent) {
			// Save summary to a dedicated log file instead of overwriting chapter
			return r.appendSummaryToLog(ctx, state, assistantContent)
		}

		path, ok := r.resolveNovelChapterPath(ctx, userMessage, assistantContent)
		if !ok {
			return loopOutcomeCompleted, false, nil
		}
		if !looksLikeChapterMarkdown(assistantContent) {
			return loopOutcomeCompleted, false, nil
		}

		outcome, did, err := r.proposeAndMaybeExecuteTool(ctx, state, "write_file", api.Args{
			"path":    path,
			"content": assistantContent,
		}, true)
		return outcome, did, err
	default:
		return loopOutcomeCompleted, false, nil
	}
}

func looksLikeChapterMarkdown(s string) bool {
	head := strings.TrimSpace(s)
	if head == "" {
		return false
	}
	if len(head) < 200 {
		return false
	}
	// Must START with a chapter title header (e.g., "# 第1章" or "第1章：标题").
	// This filters out summaries that merely mention chapters in body text.
	if !regexp.MustCompile(`(?m)^(#+\s*)?第\s*\d{1,4}\s*章`).MatchString(head) {
		return false
	}
	return true
}

// looksLikeSummary detects if content is a summary/report rather than chapter content.
func looksLikeSummary(s string) bool {
	head := strings.TrimSpace(s)
	if head == "" {
		return false
	}
	// Limit check to first 300 characters
	checkLen := 300
	if len(head) < checkLen {
		checkLen = len(head)
	}
	sample := head[:checkLen]
	// Common summary markers
	patterns := []string{"任务完成", "已完成", "总结", "Summary", "已创作", "已保存", "✅", "已完成的工作"}
	for _, p := range patterns {
		if strings.Contains(sample, p) {
			return true
		}
	}
	return false
}

// appendSummaryToLog saves summary content to a dedicated log file.
func (r *TurnRunner) appendSummaryToLog(ctx context.Context, state *api.State, content string) (loopOutcome, bool, error) {
	project, ok := resolveNovelProject(r.cfg.WorkspaceRoot)
	if !ok {
		return loopOutcomeCompleted, false, nil
	}

	summaryPath := filepath.ToSlash(filepath.Join("novel", project, "logs", "session_summaries.md"))

	// Format with timestamp
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedContent := fmt.Sprintf("\n---\n## %s\n\n%s\n", timestamp, content)

	outcome, did, err := r.proposeAndMaybeExecuteTool(ctx, state, "append_file", api.Args{
		"path":    summaryPath,
		"content": formattedContent,
	}, true)
	return outcome, did, err
}

func (r *TurnRunner) resolveNovelChapterPath(ctx context.Context, userMessage, assistantContent string) (string, bool) {
	project, ok := resolveNovelProject(r.cfg.WorkspaceRoot)
	if !ok {
		return "", false
	}

	// Prioritize assistant content (output) to find chapter number.
	// User message often contains references to OTHER chapters (e.g., "根据第4章").
	volume, chapter, ok := parseVolumeChapter(assistantContent)
	if !ok {
		// Fallback: try user message (less reliable).
		volume, chapter, ok = parseVolumeChapter(userMessage)
	}
	if !ok {
		return "", false
	}
	if volume <= 0 {
		volume = 1
	}
	if chapter <= 0 {
		return "", false
	}

	return filepath.ToSlash(filepath.Join("novel", project, "volumes", fmt.Sprintf("v%d", volume), fmt.Sprintf("c%03d.md", chapter))), true
}

func resolveNovelProject(workspaceRoot string) (string, bool) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return "", false
	}

	current := filepath.Join(workspaceRoot, "novel", ".current")
	if b, err := os.ReadFile(current); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, true
		}
	}

	novelDir := filepath.Join(workspaceRoot, "novel")
	entries, err := os.ReadDir(novelDir)
	if err != nil {
		return "", false
	}
	var projects []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := strings.TrimSpace(e.Name())
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		projects = append(projects, name)
	}
	if len(projects) == 0 {
		return "", false
	}
	sort.Strings(projects)
	return projects[0], true
}

func parseVolumeChapter(text string) (volume int, chapter int, ok bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return 0, 0, false
	}

	// v1_c4
	reVC := regexp.MustCompile(`(?i)\bv(\d+)_c(\d+)\b`)
	if m := reVC.FindStringSubmatch(s); len(m) == 3 {
		v, _ := strconv.Atoi(m[1])
		c, _ := strconv.Atoi(m[2])
		if v > 0 && c > 0 {
			return v, c, true
		}
	}

	// 第1卷第4章
	reVZHC := regexp.MustCompile(`第\s*(\d{1,3})\s*卷.*?第\s*(\d{1,4})\s*章`)
	if m := reVZHC.FindStringSubmatch(s); len(m) == 3 {
		v, _ := strconv.Atoi(m[1])
		c, _ := strconv.Atoi(m[2])
		if v > 0 && c > 0 {
			return v, c, true
		}
	}

	// 第004章
	reC := regexp.MustCompile(`第\s*(\d{1,4})\s*章`)
	if m := reC.FindStringSubmatch(s); len(m) == 2 {
		c, _ := strconv.Atoi(m[1])
		if c > 0 {
			return 1, c, true
		}
	}

	return 0, 0, false
}

func (r *TurnRunner) proposeAndMaybeExecuteTool(ctx context.Context, state *api.State, toolName string, args api.Args, stopAfter bool) (loopOutcome, bool, error) {
	tool, ok := r.cfg.Tools.Get(toolName)
	if !ok {
		return loopOutcomeCompleted, false, nil
	}

	pctx := api.PolicyContext{
		SessionID:      r.session.SessionID,
		TurnID:         r.turnID,
		ApprovalMode:   r.cfg.ApprovalMode,
		WorkspaceRoot:  r.cfg.WorkspaceRoot,
		AllowedTools:   getAllowedToolsFromState(state),
		ToolCallOrigin: api.OriginSystem,
	}

	execArgs := r.prepareExecArgs(toolName, args)

	toolCallID := fmt.Sprintf("sys_%d", time.Now().UnixNano())
	toolCall := api.ToolCallPayload{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Args:       args,
	}

	needApproval := r.cfg.Policy.NeedApproval(ctx, pctx, tool, execArgs)
	toolCall.NeedApproval = needApproval

	var preview *api.Preview
	if needApproval {
		if p, ok := tool.(tools.Previewer); ok {
			if v, err := p.Preview(ctx, execArgs); err == nil {
				preview = v
			}
		}
	}
	toolCall.Preview = preview

	r.emit(ctx, api.Event{
		Type:     api.EventToolCall,
		ToolCall: &toolCall,
	})

	if err := r.cfg.Policy.Validate(ctx, pctx, tool, execArgs); err != nil {
		r.emit(ctx, api.Event{
			Type: api.EventToolResult,
			ToolResult: &api.ToolResultPayload{
				ToolCallID: toolCallID,
				ToolName:   toolName,
				Result:     api.ToolResult{Status: "error", Error: err.Error()},
			},
		})
		return loopOutcomeCompleted, true, nil
	}

	if needApproval {
		requestID := generateRequestID()
		r.emit(ctx, api.Event{
			Type: api.EventApproval,
			Approval: &api.ApprovalPayload{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				ToolCall:   toolCall,
				Mode:       r.cfg.ApprovalMode,
			},
		})

		r.session.Pending = &api.PendingApproval{
			TurnID:    r.turnID,
			RequestID: requestID,
			ToolCall:  toolCall,
			Preview:   preview,
			CreatedAt: time.Now(),
			StopAfter: stopAfter,
		}
		if err := r.saveSession(ctx); err != nil {
			return loopOutcomeCompleted, true, err
		}
		return loopOutcomeSuspended, true, nil
	}

	result, err := tool.Execute(ctx, execArgs)
	if err != nil {
		result = api.ToolResult{Status: "error", Error: err.Error()}
	}
	r.emit(ctx, api.Event{
		Type: api.EventToolResult,
		ToolResult: &api.ToolResultPayload{
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Result:     result,
		},
	})

	r.session.Messages = append(r.session.Messages, api.LLMMessage{
		Role:       "tool",
		Content:    result.Content,
		ToolCallID: toolCallID,
	})
	if err := r.saveSession(ctx); err != nil {
		return loopOutcomeCompleted, true, err
	}
	return loopOutcomeCompleted, true, nil
}
