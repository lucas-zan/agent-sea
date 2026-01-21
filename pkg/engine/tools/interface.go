package tools

import (
	"context"
	"fmt"

	"AgentEngine/pkg/engine/api"
)

// Tool defines the unified interface for all tools exposed to the runtime.
// Tool schemas are safe to send to the model; tool execution is governed by policy.
type Tool interface {
	Name() string
	Schema() api.ToolSchema
	Risk() api.RiskLevel
	Execute(ctx context.Context, args api.Args) (api.ToolResult, error)
}

// Previewer is an optional interface for tools that can provide approval previews.
type Previewer interface {
	Preview(ctx context.Context, args api.Args) (*api.Preview, error)
}

// ParameterDef describes a single parameter for building JSON-schema tool parameters.
type ParameterDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "integer", "boolean", "array", "object"
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// BaseTool provides common functionality for tools.
type BaseTool struct {
	name        string
	description string
	params      []ParameterDef
	risk        api.RiskLevel
}

// NewBaseTool creates a new BaseTool with the given configuration.
func NewBaseTool(name, description string, params []ParameterDef, risk api.RiskLevel) BaseTool {
	return BaseTool{
		name:        name,
		description: description,
		params:      params,
		risk:        risk,
	}
}

func (b BaseTool) Name() string { return b.name }
func (b BaseTool) Risk() api.RiskLevel {
	if b.risk != "" {
		return b.risk
	}
	return api.RiskLow
}

func (b BaseTool) Schema() api.ToolSchema {
	properties := make(map[string]any)
	var required []string
	for _, p := range b.params {
		properties[p.Name] = map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	params := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return api.ToolSchema{
		Name:        b.name,
		Description: b.description,
		Parameters:  params,
	}
}

func successResult(content string, data any) api.ToolResult {
	return api.ToolResult{Content: content, Status: "success", Data: data}
}

func successText(content string) api.ToolResult { return successResult(content, nil) }

func toolError(err error) api.ToolResult {
	if err == nil {
		return api.ToolResult{Status: "error", Error: "unknown error"}
	}
	return api.ToolResult{Status: "error", Error: err.Error()}
}

func toolErrorf(format string, args ...any) api.ToolResult {
	return api.ToolResult{Status: "error", Error: fmt.Sprintf(format, args...)}
}

// GetStringArg extracts a string argument with a default value.
func GetStringArg(args api.Args, key, defaultVal string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// GetIntArg extracts an integer argument with a default value.
func GetIntArg(args api.Args, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return defaultVal
}

// GetBoolArg extracts a boolean argument with a default value.
func GetBoolArg(args api.Args, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}
