package middleware

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/prompts"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PersonaMiddleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PersonaMiddleware injects persona content from persona.md into the system prompt.
// It looks for persona.md in the workspace root.
// If persona.md doesn't exist, it uses a default persona.
type PersonaMiddleware struct {
	BaseMiddleware
	WorkspaceRoot string
	ProjectRoot   string
	AgentName     string
}

// NewPersonaMiddleware creates a new persona middleware.
func NewPersonaMiddleware(workspaceRoot string, projectRoot string, agentName string) *PersonaMiddleware {
	return &PersonaMiddleware{
		BaseMiddleware: NewBaseMiddleware("persona"),
		WorkspaceRoot:  workspaceRoot,
		ProjectRoot:    projectRoot,
		AgentName:      agentName,
	}
}

// DefaultPersona is the fallback when no persona.md exists.
const DefaultPersona = `## AI Assistant Persona

You are a helpful, intelligent AI assistant capable of:
- Understanding and executing complex tasks
- Using tools effectively to accomplish goals
- Providing clear explanations and updates on progress
- Asking for clarification when instructions are ambiguous

### Communication Style
- Be concise but thorough
- Use structured formats (lists, tables) when appropriate
- Acknowledge limitations and ask for help when needed
`

// BeforeTurn loads and injects the persona content.
func (m *PersonaMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	personaContent := m.loadPersona()

	// Build the persona block
	var personaBlock string

	// Inject conversation summary if available (from compressed history)
	if summary, ok := state.Metadata["session_summary"].(string); ok && summary != "" {
		// Load handoff prefix from prompts
		prefix := prompts.DefaultLoader.Get(prompts.CompressInjection)
		if prefix == "" {
			prefix = "Previous session context:"
		}
		personaBlock = fmt.Sprintf(`--- CONTEXT HANDOFF ---
%s

%s
--- END HANDOFF ---

`, prefix, summary)
	}

	// Add persona content
	personaBlock += fmt.Sprintf(`--- PERSONA ---
%s
--- END PERSONA ---

`, personaContent)

	// Prepend persona to existing system prompt
	state.SystemPrompt = personaBlock + state.SystemPrompt
	return nil
}

// loadPersona attempts to load persona.md from workspace, falls back to default.
func (m *PersonaMiddleware) loadPersona() string {
	var parts []string

	// Start with a baseline so there's always a persona.
	parts = append(parts, strings.TrimSpace(DefaultPersona))

	// Optional: user-level persona (~/.sea/<agent>/persona.md).
	if strings.TrimSpace(m.AgentName) != "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			userPersonaPath := filepath.Join(home, ".sea", m.AgentName, "persona.md")
			if s := readNonEmptyFile(userPersonaPath); s != "" {
				parts = append(parts, s)
			}
		}
	}

	// Optional: project-level persona (<project>/.sea/persona.md).
	if strings.TrimSpace(m.ProjectRoot) != "" {
		projectPersonaPath := filepath.Join(m.ProjectRoot, ".sea", "persona.md")
		if s := readNonEmptyFile(projectPersonaPath); s != "" {
			parts = append(parts, s)
		}
	}

	// Optional: workspace-level persona (<workspace>/persona.md) for local overrides.
	workspacePersonaPath := filepath.Join(m.WorkspaceRoot, "persona.md")
	if s := readNonEmptyFile(workspacePersonaPath); s != "" {
		parts = append(parts, s)
	}

	return strings.Join(parts, "\n\n---\n\n")
}

func readNonEmptyFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	return s
}
