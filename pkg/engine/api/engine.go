// Package api defines the stable public interface for Agent Engine.
// All external interactions should use these types.
package api

import (
	"context"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Engine Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Engine is the main entry point for all agent interactions.
// All communication happens through event streams.
type Engine interface {
	// Session management
	StartSession(ctx context.Context, opts StartOptions) (sessionID string, err error)
	GetSession(ctx context.Context, sessionID string) (SessionInfo, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)

	// Send triggers a turn, returns event stream (streaming/tool/approval/plan/done/error)
	Send(ctx context.Context, sessionID, message string) (EventStream, error)

	// Resume continues from an interrupt point (approval/cancel/modify), returns same event stream
	Resume(ctx context.Context, sessionID string, decision Decision) (EventStream, error)
}

// StartOptions configures session behavior.
type StartOptions struct {
	ApprovalMode ApprovalMode

	// EmitThinking controls whether to emit thinking events (default: false)
	EmitThinking bool

	// ActiveSkill sets the initial active skill (optional)
	ActiveSkill string
}

// SessionInfo is the public view of a session.
type SessionInfo struct {
	SessionID    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MessageCount int
	ActiveSkill  string
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Approval Mode
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ApprovalMode determines when tool calls require user approval.
type ApprovalMode string

const (
	// ModeSuggest requires approval for all tool calls (safest)
	ModeSuggest ApprovalMode = "suggest"

	// ModeAuto requires approval only for high-risk operations (default)
	ModeAuto ApprovalMode = "auto"

	// ModeFullAuto skips approval but still validates (trusted environments only)
	ModeFullAuto ApprovalMode = "full-auto"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Decision
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DecisionKind represents the type of approval decision.
type DecisionKind string

const (
	DecisionApprove DecisionKind = "approve"
	DecisionReject  DecisionKind = "reject"
	DecisionModify  DecisionKind = "modify"
)

// Decision represents a user's response to an approval request.
type Decision struct {
	Kind         DecisionKind
	RequestID    string
	ToolCallID   string
	ModifiedArgs Args // for modify kind
}

// Args is the canonical argument container for tools.
type Args = map[string]any
