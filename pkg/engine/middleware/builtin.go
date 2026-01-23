package middleware

import (
	"context"
	"fmt"
	"strings"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/skill"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SkillsMiddleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SkillsMiddleware injects active skill content into the prompt.
type SkillsMiddleware struct {
	BaseMiddleware
	SkillIndex skill.SkillIndex
}

// NewSkillsMiddleware creates a new skills middleware.
func NewSkillsMiddleware(idx skill.SkillIndex) *SkillsMiddleware {
	return &SkillsMiddleware{
		BaseMiddleware: NewBaseMiddleware("skills"),
		SkillIndex:     idx,
	}
}

// BeforeTurn injects the active skill content into the prompt.
func (m *SkillsMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	if state.ActiveSkill == "" {
		return nil
	}

	sk, err := m.SkillIndex.Load(state.ActiveSkill)
	if err != nil {
		return nil // Skill not found, skip injection
	}

	// Inject skill content with clear boundaries
	skillPrompt := fmt.Sprintf(`
--- BEGIN SKILL: %s ---
%s
--- END SKILL ---
`, sk.Name, sk.Content)

	execRules := `
--- SKILL EXECUTION RULES ---
- Follow the active skill's workflow exactly.
- If the workflow says to create/update/save files, you MUST use tools (e.g. write_file/edit_file/run_skill_script). Do not just describe what you would do.
--- END SKILL EXECUTION RULES ---
`

	state.SystemPrompt = state.SystemPrompt + skillPrompt + execRules

	// Store allowed-tools in metadata for policy to use
	if len(sk.AllowedTools) > 0 {
		if state.Metadata == nil {
			state.Metadata = make(map[string]any)
		}
		state.Metadata["allowed_tools"] = sk.AllowedTools
	}

	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MemoryMiddleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryReader reads memory entries (subset of MemoryManager for middleware).
type MemoryReader interface {
	List(ctx context.Context, source api.MemorySource) ([]api.MemoryEntry, error)
}

// MemoryMiddleware injects memory entries into the prompt.
// Note: This middleware only READS memory. Writing is done through the update_memory tool.
type MemoryMiddleware struct {
	BaseMiddleware
	Reader MemoryReader
}

// NewMemoryMiddleware creates a new memory middleware.
func NewMemoryMiddleware(reader MemoryReader) *MemoryMiddleware {
	return &MemoryMiddleware{
		BaseMiddleware: NewBaseMiddleware("memory"),
		Reader:         reader,
	}
}

// BeforeTurn injects memory summaries into the prompt.
func (m *MemoryMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	if m.Reader == nil {
		return nil
	}

	var memoryLines []string

	// Get project memory
	projectEntries, _ := m.Reader.List(ctx, api.MemorySourceProject)
	for _, e := range projectEntries {
		if len(e.Content) > 0 {
			memoryLines = append(memoryLines, fmt.Sprintf("- [project/%s] %s", e.ID, truncate(e.Content, 200)))
		}
	}

	// Get user memory
	userEntries, _ := m.Reader.List(ctx, api.MemorySourceUser)
	for _, e := range userEntries {
		if len(e.Content) > 0 {
			memoryLines = append(memoryLines, fmt.Sprintf("- [user/%s] %s", e.ID, truncate(e.Content, 200)))
		}
	}

	if len(memoryLines) == 0 {
		return nil
	}

	// Limit injection size
	if len(memoryLines) > 20 {
		memoryLines = memoryLines[:20]
	}

	memoryBlock := fmt.Sprintf(`
--- MEMORY ---
%s
--- END MEMORY ---
`, strings.Join(memoryLines, "\n"))

	state.SystemPrompt = state.SystemPrompt + memoryBlock
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PlanningMiddleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PlanReader reads plan data.
type PlanReader interface {
	Get(ctx context.Context, planID string) (*api.PlanPayload, error)
}

// PlanningMiddleware injects plan progress into the prompt.
// Note: Actual plan updates go through read_todos/write_todos tools.
type PlanningMiddleware struct {
	BaseMiddleware
	Reader PlanReader
}

// NewPlanningMiddleware creates a new planning middleware.
func NewPlanningMiddleware(reader PlanReader) *PlanningMiddleware {
	return &PlanningMiddleware{
		BaseMiddleware: NewBaseMiddleware("planning"),
		Reader:         reader,
	}
}

// BeforeTurn injects plan progress summary.
func (m *PlanningMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	if m.Reader == nil {
		return nil
	}

	planID := "plan_" + state.SessionID
	plan, err := m.Reader.Get(ctx, planID)
	if err != nil || plan == nil || len(plan.Items) == 0 {
		return nil
	}

	// Build progress summary
	total := len(plan.Items)
	done := 0
	running := 0
	for _, item := range plan.Items {
		switch item.Status {
		case api.PlanDone:
			done++
		case api.PlanRunning:
			running++
		}
	}

	progressBlock := fmt.Sprintf(`
--- PLAN PROGRESS ---
Total: %d | Done: %d | Running: %d
--- END PLAN ---
`, total, done, running)

	state.SystemPrompt = state.SystemPrompt + progressBlock
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
