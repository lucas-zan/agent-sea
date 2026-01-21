package api

import (
	"context"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EventStream Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EventStream is the unified interface for receiving engine events.
type EventStream interface {
	// Recv returns the next event. io.EOF indicates stream end.
	Recv(ctx context.Context) (Event, error)

	// Close releases stream resources.
	Close() error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Event Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EventType identifies the kind of event.
type EventType string

const (
	EventDelta      EventType = "delta"
	EventThinking   EventType = "thinking"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventApproval   EventType = "approval"
	EventPlan       EventType = "plan"
	EventDone       EventType = "done"
	EventError      EventType = "error"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Event (Strict Union)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Event is the unified output type. Only one payload should be non-nil.
type Event struct {
	Version   int       `json:"version"`
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id"`
	Seq       int64     `json:"seq"` // Monotonically increasing within a turn
	Type      EventType `json:"type"`
	Ts        time.Time `json:"ts"`

	// Strict union: exactly one payload should be non-nil
	Delta      *DeltaPayload      `json:"delta,omitempty"`
	Thinking   *ThinkingPayload   `json:"thinking,omitempty"`
	ToolCall   *ToolCallPayload   `json:"tool_call,omitempty"`
	ToolResult *ToolResultPayload `json:"tool_result,omitempty"`
	Approval   *ApprovalPayload   `json:"approval,omitempty"`
	Plan       *PlanPayload       `json:"plan,omitempty"`
	Done       *DonePayload       `json:"done,omitempty"`
	Error      *ErrorPayload      `json:"error,omitempty"`

	// Display hint for UI (optional, does not affect engine semantics)
	Display *DisplayHint `json:"display,omitempty"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Payload Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DeltaSource identifies the origin of streamed content.
type DeltaSource string

const (
	DeltaText    DeltaSource = "text"     // Normal assistant response
	DeltaToolArg DeltaSource = "tool_arg" // Tool argument being generated
)

// DeltaPayload contains streaming text increments.
type DeltaPayload struct {
	Text   string      `json:"text"`
	Source DeltaSource `json:"source,omitempty"` // Default: "text"
}

// ThinkingPayload contains progress/explanation messages.
type ThinkingPayload struct {
	Message string `json:"message"`
}

// ToolCallPayload contains tool invocation details.
type ToolCallPayload struct {
	ToolCallID   string   `json:"tool_call_id"`
	ToolName     string   `json:"tool_name"`
	Args         Args     `json:"args"`
	Preview      *Preview `json:"preview,omitempty"`
	NeedApproval bool     `json:"need_approval"` // Redundant for UI pre-rendering
}

// ToolResultPayload contains tool execution results.
type ToolResultPayload struct {
	ToolCallID string     `json:"tool_call_id"`
	ToolName   string     `json:"tool_name"`
	Result     ToolResult `json:"result"`
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "success" | "error"
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"` // Optional structured data
}

// ApprovalPayload requests user approval for a tool call.
type ApprovalPayload struct {
	RequestID  string          `json:"request_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCall   ToolCallPayload `json:"tool_call"`
	Mode       ApprovalMode    `json:"mode"`
}

// DonePayload marks turn completion.
type DonePayload struct {
	Reason string `json:"reason,omitempty"` // e.g., "completed", "rejected", "canceled", "error"
}

// ErrorPayload contains error information.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Plan Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PlanItemStatus represents the state of a plan item.
type PlanItemStatus string

const (
	PlanPending PlanItemStatus = "pending"
	PlanRunning PlanItemStatus = "running"
	PlanDone    PlanItemStatus = "done"
	PlanBlocked PlanItemStatus = "blocked"
	PlanErrored PlanItemStatus = "errored"
)

// PlanItem represents a single task in a plan.
type PlanItem struct {
	ID     int            `json:"id"`
	Text   string         `json:"text"`
	Status PlanItemStatus `json:"status"`
}

// PlanPayload contains the full plan state.
type PlanPayload struct {
	PlanID     string     `json:"plan_id"` // canonical: "plan_<sessionID>"
	Items      []PlanItem `json:"items"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // Correlation for UI
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Preview Types
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PreviewKind identifies the type of preview content.
type PreviewKind string

const (
	PreviewDiff    PreviewKind = "diff"
	PreviewCommand PreviewKind = "command"
	PreviewFiles   PreviewKind = "files"
	PreviewText    PreviewKind = "text"
)

// Preview contains information for approval UI.
type Preview struct {
	Kind     PreviewKind `json:"kind"`
	Summary  string      `json:"summary"`
	Content  string      `json:"content,omitempty"`  // diff or command text
	Affected []string    `json:"affected,omitempty"` // affected paths
	RiskHint string      `json:"risk_hint,omitempty"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Display Hint
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DisplayHint provides UI rendering suggestions.
type DisplayHint struct {
	Level  string `json:"level,omitempty"`  // "debug" | "info" | "warning" | "error"
	Style  string `json:"style,omitempty"`  // "inline" | "block" | "collapsible"
	Sticky bool   `json:"sticky,omitempty"` // Keep visible in UI
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Standard Error Codes
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const (
	ErrInvalidSession    = "invalid_session"
	ErrTurnInProgress    = "turn_in_progress"
	ErrNoPendingApproval = "no_pending_approval"
	ErrApprovalMismatch  = "approval_mismatch"
	ErrToolNotFound      = "tool_not_found"
	ErrToolArgsInvalid   = "tool_args_invalid"
	ErrPolicyDenied      = "policy_denied"
	ErrWorkspaceEscape   = "workspace_escape"
	ErrToolExecuteFailed = "tool_execute_failed"
	ErrStoreError        = "store_error"
)
