package middleware

import (
	"context"
	"fmt"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BasePromptMiddleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BasePromptMiddleware injects the base system prompt with workspace context.
type BasePromptMiddleware struct {
	BaseMiddleware
	WorkspaceRoot string
}

// NewBasePromptMiddleware creates a new base prompt middleware.
func NewBasePromptMiddleware(workspaceRoot string) *BasePromptMiddleware {
	return &BasePromptMiddleware{
		BaseMiddleware: NewBaseMiddleware("base_prompt"),
		WorkspaceRoot:  workspaceRoot,
	}
}

// BeforeTurn injects the base system prompt.
func (m *BasePromptMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	basePrompt := fmt.Sprintf(`You are an AI assistant with access to tools for file operations and task management.

## Working Directory
Your working directory is: %s
All file paths you provide should be relative to this workspace directory.
When you call tools, the path "." refers to this directory.

## Mandatory Workflow (Every User Message)
1) Call understand_intent first.
2) Call read_todos to see if there is an existing plan.
3) If the user request continues the existing plan, continue execution and keep task statuses accurate.
4) Otherwise, create a new plan with write_todos (mode "set") before executing.

## Tool Usage Guidelines

### File Operations - ALWAYS provide required arguments!
	- ls: List directory contents. Example: {"path": "."} or {"path": "novel"}
	- glob: Find files by pattern. Example: {"pattern": "**/*.md"} or {"pattern": "*.json", "path": "."}
	- grep: Search text in files. Example: {"pattern": "keyword", "path": "."}
	- read_file: Read file contents. Example: {"path": "persona.md"} or {"path": "novel/<project_name>/outline.md"}
	- write_file: Create/overwrite files. Example: {"path": "test.md", "content": "Hello"}
	- edit_file: Modify existing files. Example: {"path": "test.md", "old_text": "old", "new_text": "new"}
	- shell: Execute shell commands. Example: {"command": "ls -la"}

### Task Management
- read_todos: View current task list
- write_todos: Update task list

### Skills
- list_skills: Show available skills
- activate_skill: Enable a skill for specialized tasks

## IMPORTANT
- Always provide ALL required arguments when calling tools
- Use specific paths relative to the workspace root
- If a tool returns an error, check that you provided the correct arguments

`, m.WorkspaceRoot)

	// Prepend base prompt (other middlewares will append to it)
	state.SystemPrompt = basePrompt + state.SystemPrompt
	return nil
}
