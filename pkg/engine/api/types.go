package api

import "time"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Policy Context
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PolicyContext is the input for all policy decisions.
// Keep it stable and serializable for audit/replay.
type PolicyContext struct {
	SessionID string
	TurnID    string

	ApprovalMode ApprovalMode

	// AllowedTools from skill frontmatter (allowlist for non-system tools)
	// Empty means no skill-level restriction.
	AllowedTools []string

	// ToolCallOrigin indicates where the tool call came from.
	ToolCallOrigin ToolCallOrigin

	WorkspaceRoot string
}

// ToolCallOrigin identifies the source of a tool call.
type ToolCallOrigin string

const (
	OriginModel      ToolCallOrigin = "model"
	OriginMiddleware ToolCallOrigin = "middleware"
	OriginSystem     ToolCallOrigin = "system"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Risk Level
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RiskLevel indicates the risk level of a tool.
type RiskLevel string

const (
	RiskNone RiskLevel = "none"
	RiskLow  RiskLevel = "low"
	RiskHigh RiskLevel = "high"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Tool Definitions
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ToolDefinition is engine-internal metadata for UI rendering and policy decisions.
// MUST NOT be passed to the LLM directly.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema-like
	Risk        RiskLevel
}

// ToolSchema is the LLM-exposed tool schema (safe to send to model).
type ToolSchema struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema-like
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// System Tool Allowlist
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SystemToolAllowlist contains tools that bypass skill allowed-tools restrictions.
// These are always visible and callable (but still subject to NeedApproval/Validate).
var SystemToolAllowlist = map[string]bool{
	"list_skills":       true,
	"read_skill":        true,
	"activate_skill":    true,
	"read_memory":       true,
	"update_memory":     true,
	"read_todos":        true,
	"write_todos":       true,
	"understand_intent": true,
}

// IsSystemTool checks if a tool is in the system allowlist.
func IsSystemTool(name string) bool {
	return SystemToolAllowlist[name]
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// LLM Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LLMMessage represents a message in the LLM conversation.
type LLMMessage struct {
	Role       string        `json:"role"` // "system" | "user" | "assistant" | "tool"
	Content    string        `json:"content"`
	ToolCalls  []LLMToolCall `json:"tool_calls,omitempty"`   // for assistant role
	ToolCallID string        `json:"tool_call_id,omitempty"` // for tool role
}

// LLMToolCall represents a tool call from the LLM.
type LLMToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // JSON string
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Session Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Session is the persisted session record.
type Session struct {
	SessionID   string            `json:"session_id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ActiveSkill string            `json:"active_skill,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	Summary  string           `json:"summary,omitempty"` // Compressed history summary
	Messages []LLMMessage     `json:"messages"`
	Pending  *PendingApproval `json:"pending,omitempty"`
}

// PendingApproval stores the state needed to resume after approval.
type PendingApproval struct {
	TurnID    string          `json:"turn_id"`
	RequestID string          `json:"request_id"`
	ToolCall  ToolCallPayload `json:"tool_call"`
	Preview   *Preview        `json:"preview,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	StopAfter bool            `json:"stop_after,omitempty"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Middleware Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// State is the per-turn mutable state passed across middleware.
type State struct {
	SessionID   string
	TurnID      string
	ActiveSkill string

	SystemPrompt string
	Messages     []LLMMessage

	Metadata map[string]any
}

// TurnOutcome represents how a turn completed.
type TurnOutcome string

const (
	TurnDone     TurnOutcome = "done"
	TurnError    TurnOutcome = "error"
	TurnCanceled TurnOutcome = "canceled"
)

// TurnSummary is an immutable view of a completed turn.
type TurnSummary struct {
	SessionID string
	TurnID    string

	Outcome       TurnOutcome
	AssistantText string

	ToolCalls  []ToolCallRef
	Approvals  []ApprovalRef
	Error      *ErrorPayload
	StartedAt  time.Time
	FinishedAt time.Time
}

// ToolCallRef is a reference to a tool call.
type ToolCallRef struct {
	ToolCallID string
	ToolName   string
}

// ApprovalRef is a reference to an approval request.
type ApprovalRef struct {
	RequestID  string
	ToolCallID string
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Skill Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SkillMeta contains indexed skill metadata.
type SkillMeta struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	License       string   `json:"license,omitempty"`
	Compatibility string   `json:"compatibility,omitempty"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	Path          string   `json:"path"`
}

// Skill is the full content loaded by SkillIndex.Load().
type Skill struct {
	SkillMeta
	Content    string            `json:"content"` // Markdown body
	Scripts    []string          `json:"scripts,omitempty"`
	References []string          `json:"references,omitempty"`
	Assets     []string          `json:"assets,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Memory Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryType categorizes memory entries.
type MemoryType string

const (
	MemoryFact       MemoryType = "fact"
	MemoryPreference MemoryType = "preference"
	MemoryDecision   MemoryType = "decision"
	MemoryLesson     MemoryType = "lesson"
)

// MemorySource indicates where memory is stored.
type MemorySource string

const (
	MemorySourceUser    MemorySource = "user"
	MemorySourceProject MemorySource = "project"
)

// MemoryEntry represents a single memory item.
type MemoryEntry struct {
	ID        string       `json:"id"`
	Type      MemoryType   `json:"type"`
	Content   string       `json:"content"`
	Source    MemorySource `json:"source"`
	Tags      []string     `json:"tags,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}
