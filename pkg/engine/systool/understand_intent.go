package systool

import (
	"context"
	"encoding/json"
	"fmt"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/logger"
)

// UnderstandIntentTool structures LLM's intent analysis into a parseable format.
// This enables downstream middleware to make decisions based on intent.
type UnderstandIntentTool struct{}

// IntentArgs is the input for understand_intent.
type IntentArgs struct {
	Summary     string   `json:"summary"`      // One-line summary
	Category    string   `json:"category"`     // query/create/modify/delete
	Complexity  string   `json:"complexity"`   // simple/complex
	RequiredCtx []string `json:"required_ctx"` // Files needed for context
}

func (t *UnderstandIntentTool) Name() string        { return "understand_intent" }
func (t *UnderstandIntentTool) Risk() api.RiskLevel { return api.RiskNone }

func (t *UnderstandIntentTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        "understand_intent",
		Description: "Record your understanding of user's intent. MUST be called as the first step for every request.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{
					"type":        "string",
					"description": "One-line summary of user's intent",
				},
				"category": map[string]any{
					"type":        "string",
					"enum":        []string{"query", "create", "modify", "delete"},
					"description": "Type of task: query (read-only), create (new content), modify (edit existing), delete (remove)",
				},
				"complexity": map[string]any{
					"type":        "string",
					"enum":        []string{"simple", "complex"},
					"description": "simple = can be done in 1-2 steps, complex = needs task decomposition",
				},
				"required_ctx": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "List of file paths needed to understand context (read before executing)",
				},
			},
			"required": []string{"summary", "category", "complexity"},
		},
	}
}

func (t *UnderstandIntentTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	// Parse args
	var intent IntentArgs
	argsJSON, _ := json.Marshal(args)
	if err := json.Unmarshal(argsJSON, &intent); err != nil {
		return api.ToolResult{
			Status: "error",
			Error:  "Invalid intent format: " + err.Error(),
		}, nil
	}

	// Validate required fields
	if intent.Summary == "" {
		return api.ToolResult{Status: "error", Error: "summary is required"}, nil
	}
	if intent.Category == "" {
		return api.ToolResult{Status: "error", Error: "category is required"}, nil
	}
	if intent.Complexity == "" {
		return api.ToolResult{Status: "error", Error: "complexity is required"}, nil
	}

	// Log the intent
	logger.Info("Intent", "User intent understood", map[string]interface{}{
		"summary":    intent.Summary,
		"category":   intent.Category,
		"complexity": intent.Complexity,
		"ctx_files":  len(intent.RequiredCtx),
	})

	// Build response
	response := fmt.Sprintf(`✅ Intent recorded

**Summary**: %s
**Category**: %s
**Complexity**: %s`, intent.Summary, intent.Category, intent.Complexity)

	if len(intent.RequiredCtx) > 0 {
		response += "\n**Required context**: "
		for i, f := range intent.RequiredCtx {
			if i > 0 {
				response += ", "
			}
			response += f
		}
	}

	if intent.Complexity == "complex" {
		response += "\n\n➡️ Next: use write_todos to create a task list"
	} else {
		response += "\n\n➡️ Next: execute the task directly"
	}

	return api.ToolResult{
		Content: response,
		Status:  "success",
		Data: map[string]any{
			"intent": intent,
		},
	}, nil
}
