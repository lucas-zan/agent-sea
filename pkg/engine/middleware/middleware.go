// Package mw provides the new middleware interface aligned with CORE_DESIGN.md.
package middleware

import (
	"context"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Middleware Interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Middleware defines the interface for agent middleware.
// Middleware can modify agent state before/after processing and observe events.
type Middleware interface {
	// Name returns the middleware identifier.
	Name() string

	// BeforeTurn is called before agent processing.
	// Can modify state (system prompt, messages, etc.)
	BeforeTurn(ctx context.Context, state *api.State) error

	// OnEvent is called for each event during the turn.
	// Used for memory proposals, plan proposals, audit enhancement.
	OnEvent(ctx context.Context, state *api.State, e api.Event) error

	// AfterTurn is called after turn completion.
	AfterTurn(ctx context.Context, state *api.State, summary api.TurnSummary) error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Chain
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Chain runs multiple middleware in order.
type Chain struct {
	middlewares []Middleware
}

// NewChain creates a middleware chain.
func NewChain(middlewares ...Middleware) *Chain {
	return &Chain{middlewares: middlewares}
}

// Add appends middleware to the chain.
func (c *Chain) Add(m Middleware) {
	c.middlewares = append(c.middlewares, m)
}

// BeforeTurn runs all middleware BeforeTurn in order.
func (c *Chain) BeforeTurn(ctx context.Context, state *api.State) error {
	for _, m := range c.middlewares {
		if err := m.BeforeTurn(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

// OnEvent runs all middleware OnEvent in order.
func (c *Chain) OnEvent(ctx context.Context, state *api.State, e api.Event) error {
	for _, m := range c.middlewares {
		if err := m.OnEvent(ctx, state, e); err != nil {
			return err
		}
	}
	return nil
}

// AfterTurn runs all middleware AfterTurn in reverse order.
func (c *Chain) AfterTurn(ctx context.Context, state *api.State, summary api.TurnSummary) error {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		if err := c.middlewares[i].AfterTurn(ctx, state, summary); err != nil {
			return err
		}
	}
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Base Middleware
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BaseMiddleware provides no-op implementations of all methods.
type BaseMiddleware struct {
	name string
}

func NewBaseMiddleware(name string) BaseMiddleware {
	return BaseMiddleware{name: name}
}

func (m BaseMiddleware) Name() string { return m.name }

func (m BaseMiddleware) BeforeTurn(ctx context.Context, state *api.State) error {
	return nil
}

func (m BaseMiddleware) OnEvent(ctx context.Context, state *api.State, e api.Event) error {
	return nil
}

func (m BaseMiddleware) AfterTurn(ctx context.Context, state *api.State, summary api.TurnSummary) error {
	return nil
}
