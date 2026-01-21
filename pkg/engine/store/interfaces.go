// Package store provides storage abstractions for the agent engine.
package store

import (
	"context"
	"errors"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Store Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Store is a generic key-value store interface.
type Store[T any] interface {
	Get(ctx context.Context, id string) (T, error)
	Put(ctx context.Context, id string, value T) error
	Del(ctx context.Context, id string) error
	List(ctx context.Context) ([]string, error)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Specialized Store Aliases
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SessionStore stores Session records.
type SessionStore = Store[*api.Session]

// PlanStore stores Plan records.
type PlanStore = Store[*api.PlanPayload]

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EventLog Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EventLog is an append-only event log for auditing and replay.
type EventLog interface {
	// Append adds an event to the log.
	Append(ctx context.Context, e api.Event) error

	// Stream returns an event stream for a session (for replay).
	Stream(ctx context.Context, sessionID string) (api.EventStream, error)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Standard Errors
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

var (
	ErrNotFound        = errors.New("not found")
	ErrWorkspaceEscape = errors.New("path escapes workspace boundary")
	ErrInvalidPath     = errors.New("invalid path")
)
