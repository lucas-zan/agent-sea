// Package systool provides system tools for the agent engine.
package systool

import (
	"context"
	"encoding/json"
	"fmt"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/skill"
	"AgentEngine/pkg/engine/store"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Skills Tools
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ListSkillsTool lists all available skills.
type ListSkillsTool struct {
	SkillIndex skill.SkillIndex
}

func (t *ListSkillsTool) Name() string        { return "list_skills" }
func (t *ListSkillsTool) Description() string { return "List all available skills" }
func (t *ListSkillsTool) Risk() api.RiskLevel { return api.RiskNone }
func (t *ListSkillsTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListSkillsTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	skills := t.SkillIndex.List()

	data := map[string]any{
		"skills": skills,
	}
	content, _ := json.MarshalIndent(data, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: data}, nil
}

// ReadSkillTool reads skill content.
type ReadSkillTool struct {
	SkillIndex skill.SkillIndex
}

func (t *ReadSkillTool) Name() string        { return "read_skill" }
func (t *ReadSkillTool) Description() string { return "Read skill content by name" }
func (t *ReadSkillTool) Risk() api.RiskLevel { return api.RiskNone }
func (t *ReadSkillTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name"},
				"section": map[string]any{
					"type":        "string",
					"description": "Which part to return: all/frontmatter/content/scripts/references/assets",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *ReadSkillTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return api.ToolResult{Status: "error", Error: "name argument required"}, nil
	}

	section, _ := args["section"].(string)
	if section == "" {
		section = "all"
	}

	sk, err := t.SkillIndex.Load(name)
	if err != nil {
		return api.ToolResult{Status: "error", Error: err.Error()}, nil
	}

	var data any
	switch section {
	case "frontmatter":
		data = map[string]any{"skill": sk.SkillMeta}
	case "content":
		data = map[string]any{"content": sk.Content}
	case "scripts":
		data = map[string]any{"scripts": sk.Scripts}
	case "references":
		data = map[string]any{"references": sk.References}
	case "assets":
		data = map[string]any{"assets": sk.Assets}
	default:
		data = map[string]any{"skill": sk}
	}

	content, _ := json.MarshalIndent(data, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: data}, nil
}

// ActivateSkillTool activates a skill for the session.
type ActivateSkillTool struct {
	SkillIndex skill.SkillIndex
}

func (t *ActivateSkillTool) Name() string        { return "activate_skill" }
func (t *ActivateSkillTool) Description() string { return "Activate a skill for the current session" }
func (t *ActivateSkillTool) Risk() api.RiskLevel { return api.RiskLow }
func (t *ActivateSkillTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name"},
			},
			"required": []string{"name"},
		},
	}
}

func (t *ActivateSkillTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return api.ToolResult{Status: "error", Error: "name argument required"}, nil
	}

	// Verify skill exists
	var meta api.SkillMeta
	if g, ok := t.SkillIndex.(interface {
		Get(name string) (api.SkillMeta, bool)
	}); ok {
		m, exists := g.Get(name)
		if !exists {
			return api.ToolResult{Status: "error", Error: fmt.Sprintf("skill not found: %s", name)}, nil
		}
		meta = m
	} else {
		sk, err := t.SkillIndex.Load(name)
		if err != nil {
			return api.ToolResult{Status: "error", Error: fmt.Sprintf("skill not found: %s", name)}, nil
		}
		meta = sk.SkillMeta
	}

	data := map[string]any{"active": meta}
	content, _ := json.MarshalIndent(data, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: data}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Plan Tools
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ReadTodosTool reads the current plan.
type ReadTodosTool struct {
	PlanStore store.PlanStore
}

func (t *ReadTodosTool) Name() string        { return "read_todos" }
func (t *ReadTodosTool) Description() string { return "Read the current plan/todos" }
func (t *ReadTodosTool) Risk() api.RiskLevel { return api.RiskNone }
func (t *ReadTodosTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Optional explicit plan id (default: plan_<sessionID>)"},
			},
		},
	}
}

func (t *ReadTodosTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	planID, _ := args["plan_id"].(string)
	if planID == "" {
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return api.ToolResult{Status: "error", Error: "session_id missing (engine should inject)"}, nil
		}
		planID = "plan_" + sessionID
	}

	plan, err := t.PlanStore.Get(ctx, planID)
	if err != nil {
		if err == store.ErrNotFound {
			data := map[string]any{"plan_id": planID, "items": []any{}}
			content, _ := json.MarshalIndent(data, "", "  ")
			return api.ToolResult{Content: string(content), Status: "success", Data: data}, nil
		}
		return api.ToolResult{Status: "error", Error: err.Error()}, nil
	}

	content, _ := json.MarshalIndent(plan, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: plan}, nil
}

// WriteTodosTool writes to the plan.
type WriteTodosTool struct {
	PlanStore store.PlanStore
}

func (t *WriteTodosTool) Name() string        { return "write_todos" }
func (t *WriteTodosTool) Description() string { return "Create or update the plan/todos" }
func (t *WriteTodosTool) Risk() api.RiskLevel { return api.RiskHigh }
func (t *WriteTodosTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Optional explicit plan id (default: plan_<sessionID>)"},
				"mode":    map[string]any{"type": "string", "description": "set | append | patch"},
				"items": map[string]any{
					"type":        "array",
					"description": "Items for set/append",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":     map[string]any{"type": "integer"},
							"text":   map[string]any{"type": "string"},
							"status": map[string]any{"type": "string"},
						},
					},
				},
				"patches": map[string]any{
					"type":        "array",
					"description": "Patches for patch mode",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":     map[string]any{"type": "integer"},
							"text":   map[string]any{"type": "string"},
							"status": map[string]any{"type": "string"},
							"delete": map[string]any{"type": "boolean"},
						},
					},
				},
			},
		},
	}
}

func (t *WriteTodosTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	planID, _ := args["plan_id"].(string)
	if planID == "" {
		sessionID, _ := args["session_id"].(string)
		if sessionID == "" {
			return api.ToolResult{Status: "error", Error: "session_id missing (engine should inject)"}, nil
		}
		planID = "plan_" + sessionID
	}

	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "set"
	}

	// Parse items
	var newItems []api.PlanItem
	if itemsRaw, ok := args["items"].([]any); ok {
		for _, item := range itemsRaw {
			if itemMap, ok := item.(map[string]any); ok {
				pi := api.PlanItem{}
				if id, ok := itemMap["id"].(float64); ok {
					pi.ID = int(id)
				}
				if text, ok := itemMap["text"].(string); ok {
					pi.Text = text
				}
				if status, ok := itemMap["status"].(string); ok {
					pi.Status = api.PlanItemStatus(status)
				} else {
					pi.Status = api.PlanPending
				}
				newItems = append(newItems, pi)
			}
		}
	}

	var plan *api.PlanPayload

	switch mode {
	case "set":
		plan = &api.PlanPayload{
			PlanID: planID,
			Items:  newItems,
		}

	case "append":
		existing, err := t.PlanStore.Get(ctx, planID)
		if err != nil && err != store.ErrNotFound {
			return api.ToolResult{Status: "error", Error: err.Error()}, nil
		}
		if existing == nil {
			existing = &api.PlanPayload{PlanID: planID}
		}
		// Auto-assign IDs for append
		maxID := 0
		for _, item := range existing.Items {
			if item.ID > maxID {
				maxID = item.ID
			}
		}
		for i := range newItems {
			if newItems[i].ID == 0 {
				maxID++
				newItems[i].ID = maxID
			}
		}
		existing.Items = append(existing.Items, newItems...)
		plan = existing

	case "patch":
		existing, err := t.PlanStore.Get(ctx, planID)
		if err != nil {
			return api.ToolResult{Status: "error", Error: err.Error()}, nil
		}

		// Apply patches
		if patchesRaw, ok := args["patches"].([]any); ok {
			for _, p := range patchesRaw {
				if patchMap, ok := p.(map[string]any); ok {
					id := 0
					if idFloat, ok := patchMap["id"].(float64); ok {
						id = int(idFloat)
					}
					if id == 0 {
						continue
					}

					for i := range existing.Items {
						if existing.Items[i].ID == id {
							if text, ok := patchMap["text"].(string); ok {
								existing.Items[i].Text = text
							}
							if status, ok := patchMap["status"].(string); ok {
								existing.Items[i].Status = api.PlanItemStatus(status)
							}
							if del, ok := patchMap["delete"].(bool); ok && del {
								existing.Items = append(existing.Items[:i], existing.Items[i+1:]...)
							}
							break
						}
					}
				}
			}
		}
		plan = existing

	default:
		return api.ToolResult{Status: "error", Error: "invalid mode: " + mode}, nil
	}

	// Validate unique IDs
	idSeen := make(map[int]bool)
	for _, item := range plan.Items {
		if idSeen[item.ID] {
			return api.ToolResult{Status: "error", Error: fmt.Sprintf("duplicate item ID: %d", item.ID)}, nil
		}
		idSeen[item.ID] = true
	}

	// Save
	if err := t.PlanStore.Put(ctx, planID, plan); err != nil {
		return api.ToolResult{Status: "error", Error: err.Error()}, nil
	}

	content, _ := json.MarshalIndent(plan, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: plan}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Memory Tools
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryManager interface for memory operations.
type MemoryManager interface {
	List(ctx context.Context, source api.MemorySource) ([]api.MemoryEntry, error)
	Search(ctx context.Context, query string) ([]api.MemoryEntry, error)
	Add(ctx context.Context, entry api.MemoryEntry) error
	Update(ctx context.Context, entry api.MemoryEntry) error
	Delete(ctx context.Context, id string) error
}

// ReadMemoryTool reads memory entries.
type ReadMemoryTool struct {
	Manager MemoryManager
}

func (t *ReadMemoryTool) Name() string        { return "read_memory" }
func (t *ReadMemoryTool) Description() string { return "Read memory entries" }
func (t *ReadMemoryTool) Risk() api.RiskLevel { return api.RiskNone }
func (t *ReadMemoryTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "user | project | all"},
				"query":  map[string]any{"type": "string", "description": "Search query (optional)"},
				"limit":  map[string]any{"type": "integer", "description": "Max results (default 20)"},
			},
		},
	}
}

func (t *ReadMemoryTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	source, _ := args["source"].(string)
	query, _ := args["query"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	var entries []api.MemoryEntry
	var err error

	if query != "" {
		entries, err = t.Manager.Search(ctx, query)
	} else if source != "" && source != "all" {
		entries, err = t.Manager.List(ctx, api.MemorySource(source))
	} else {
		// List all
		userEntries, _ := t.Manager.List(ctx, api.MemorySourceUser)
		projectEntries, _ := t.Manager.List(ctx, api.MemorySourceProject)
		entries = append(userEntries, projectEntries...)
	}

	if err != nil {
		return api.ToolResult{Status: "error", Error: err.Error()}, nil
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	data := map[string]any{"entries": entries}
	content, _ := json.MarshalIndent(data, "", "  ")
	return api.ToolResult{Content: string(content), Status: "success", Data: data}, nil
}

// UpdateMemoryTool modifies memory entries.
type UpdateMemoryTool struct {
	Manager MemoryManager
}

func (t *UpdateMemoryTool) Name() string        { return "update_memory" }
func (t *UpdateMemoryTool) Description() string { return "Add, update, or delete memory entries" }
func (t *UpdateMemoryTool) Risk() api.RiskLevel { return api.RiskHigh }
func (t *UpdateMemoryTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op": map[string]any{"type": "string", "description": "add | update | delete"},
				"entry": map[string]any{
					"type":        "object",
					"description": "Entry for add/update",
					"properties": map[string]any{
						"id":      map[string]any{"type": "string"},
						"type":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
						"source":  map[string]any{"type": "string"},
						"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
				"id": map[string]any{"type": "string", "description": "Entry ID for delete"},
			},
			"required": []string{"op"},
		},
	}
}

func (t *UpdateMemoryTool) Execute(ctx context.Context, args api.Args) (api.ToolResult, error) {
	op, _ := args["op"].(string)
	if op == "" {
		return api.ToolResult{Status: "error", Error: "op argument required (add/update/delete)"}, nil
	}

	switch op {
	case "add", "update":
		entryMap, ok := args["entry"].(map[string]any)
		if !ok {
			return api.ToolResult{Status: "error", Error: "entry argument required"}, nil
		}

		entry := api.MemoryEntry{}
		if id, ok := entryMap["id"].(string); ok {
			entry.ID = id
		}
		if t, ok := entryMap["type"].(string); ok {
			entry.Type = api.MemoryType(t)
		}
		if c, ok := entryMap["content"].(string); ok {
			entry.Content = c
		}
		if s, ok := entryMap["source"].(string); ok {
			entry.Source = api.MemorySource(s)
		}
		if tags, ok := entryMap["tags"].([]any); ok {
			for _, tag := range tags {
				if s, ok := tag.(string); ok {
					entry.Tags = append(entry.Tags, s)
				}
			}
		}

		var err error
		if op == "add" {
			err = t.Manager.Add(ctx, entry)
		} else {
			err = t.Manager.Update(ctx, entry)
		}
		if err != nil {
			return api.ToolResult{Status: "error", Error: err.Error()}, nil
		}

	case "delete":
		id, ok := args["id"].(string)
		if !ok || id == "" {
			return api.ToolResult{Status: "error", Error: "id argument required for delete"}, nil
		}
		if err := t.Manager.Delete(ctx, id); err != nil {
			return api.ToolResult{Status: "error", Error: err.Error()}, nil
		}

	default:
		return api.ToolResult{Status: "error", Error: "invalid op: " + op}, nil
	}

	return api.ToolResult{Content: `{"ok": true}`, Status: "success", Data: map[string]any{"ok": true}}, nil
}
