package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/policy"
	"AgentEngine/pkg/engine/store"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Engine Implementation
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EngineConfig holds engine configuration.
type EngineConfig struct {
	LLM         LLM
	Tools       ToolRegistry
	Policy      policy.Policy
	Middlewares []Middleware

	WorkspaceRoot string

	// Optional stores. If nil, file-backed stores under <WorkspaceRoot>/workspace/ will be used.
	SessionStore store.SessionStore
	PlanStore    store.PlanStore
	EventLog     store.EventLog

	// Compression settings
	AutoCompressThreshold int // 0 = disabled
	CompressKeepTurns     int // Default: 3

	// Filter historical tool_calls/tool messages before sending to LLM
	FilterHistoryTools bool
}

// Engine implements api.Engine interface.
type Engine struct {
	cfg EngineConfig

	sessionStore store.SessionStore
	planStore    store.PlanStore
	eventLog     store.EventLog

	// Track active turns per session
	activeTurns map[string]*TurnRunner
	turnsMu     sync.Mutex
}

// NewEngine creates a new engine instance.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	sessionStore := cfg.SessionStore
	planStore := cfg.PlanStore
	eventLog := cfg.EventLog

	if sessionStore == nil {
		ss, err := store.NewFileSessionStore(cfg.WorkspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to create session store: %w", err)
		}
		sessionStore = ss
	}

	if planStore == nil {
		ps, err := store.NewFilePlanStore(cfg.WorkspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to create plan store: %w", err)
		}
		planStore = ps
	}

	if eventLog == nil {
		el, err := store.NewJSONLEventLog(cfg.WorkspaceRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to create event log: %w", err)
		}
		eventLog = el
	}

	return &Engine{
		cfg:          cfg,
		sessionStore: sessionStore,
		planStore:    planStore,
		eventLog:     eventLog,
		activeTurns:  make(map[string]*TurnRunner),
	}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Session Management
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// StartSession creates a new session.
func (e *Engine) StartSession(ctx context.Context, opts api.StartOptions) (string, error) {
	sessionID := generateSessionID()

	metadata := make(map[string]string)
	if opts.ApprovalMode != "" {
		metadata["approval_mode"] = string(opts.ApprovalMode)
	}
	if opts.EmitThinking {
		metadata["emit_thinking"] = "true"
	}

	session := &api.Session{
		SessionID:   sessionID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ActiveSkill: opts.ActiveSkill,
		Metadata:    metadata,
		Messages:    []api.LLMMessage{},
	}

	if err := e.sessionStore.Put(ctx, sessionID, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return sessionID, nil
}

// GetSession retrieves session info.
func (e *Engine) GetSession(ctx context.Context, sessionID string) (api.SessionInfo, error) {
	session, err := e.sessionStore.Get(ctx, sessionID)
	if err != nil {
		if err == store.ErrNotFound {
			return api.SessionInfo{}, fmt.Errorf("%s: %s", api.ErrInvalidSession, sessionID)
		}
		return api.SessionInfo{}, err
	}

	return api.SessionInfo{
		SessionID:    session.SessionID,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
		MessageCount: len(session.Messages),
		ActiveSkill:  session.ActiveSkill,
	}, nil
}

// ListSessions lists all sessions.
func (e *Engine) ListSessions(ctx context.Context) ([]api.SessionInfo, error) {
	ids, err := e.sessionStore.List(ctx)
	if err != nil {
		return nil, err
	}

	var infos []api.SessionInfo
	for _, id := range ids {
		info, err := e.GetSession(ctx, id)
		if err != nil {
			continue // Skip invalid sessions
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// CompressSession compresses the history of a session.
// It generates a summary of older messages and keeps only the last N turns.
func (e *Engine) CompressSession(ctx context.Context, sessionID string, keepTurns int) (*CompressResult, error) {
	// Check no active turn
	e.turnsMu.Lock()
	if _, exists := e.activeTurns[sessionID]; exists {
		e.turnsMu.Unlock()
		return nil, fmt.Errorf("%s: %s", api.ErrTurnInProgress, sessionID)
	}
	e.turnsMu.Unlock()

	// Load session
	session, err := e.sessionStore.Get(ctx, sessionID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("%s: %s", api.ErrInvalidSession, sessionID)
		}
		return nil, err
	}

	oldCount := len(session.Messages)

	// Compress - force when manually triggered
	cfg := CompressConfig{
		KeepTurns:     keepTurns,
		MaxMessages:   20,
		ForceCompress: true, // Manual compress should always work
	}
	if err := CompressHistory(ctx, e.cfg.LLM, session, cfg); err != nil {
		return nil, err
	}

	// Save session
	session.UpdatedAt = time.Now()
	if err := e.sessionStore.Put(ctx, sessionID, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return &CompressResult{
		MessagesRemoved: oldCount - len(session.Messages),
		MessagesKept:    len(session.Messages),
		SummaryLength:   len(session.Summary),
		Summary:         session.Summary,
	}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Turn Execution
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Send triggers a turn with a user message.
func (e *Engine) Send(ctx context.Context, sessionID, message string) (api.EventStream, error) {
	// Check for existing active turn
	e.turnsMu.Lock()
	if _, exists := e.activeTurns[sessionID]; exists {
		e.turnsMu.Unlock()
		return nil, fmt.Errorf("%s: %s", api.ErrTurnInProgress, sessionID)
	}

	// Load session
	session, err := e.sessionStore.Get(ctx, sessionID)
	if err != nil {
		e.turnsMu.Unlock()
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("%s: %s", api.ErrInvalidSession, sessionID)
		}
		return nil, err
	}

	// Check for pending approval
	if session.Pending != nil {
		e.turnsMu.Unlock()
		return nil, fmt.Errorf("%s: pending approval exists", api.ErrTurnInProgress)
	}

	approvalMode := api.ModeAuto
	emitThinking := false
	if session.Metadata != nil {
		if v := session.Metadata["approval_mode"]; v != "" {
			approvalMode = api.ApprovalMode(v)
		}
		if session.Metadata["emit_thinking"] == "true" {
			emitThinking = true
		}
	}

	// Create turn runner
	runner := NewTurnRunner(TurnRunnerConfig{
		LLM:                   e.cfg.LLM,
		Tools:                 e.cfg.Tools,
		Policy:                e.cfg.Policy,
		SessionStore:          e.sessionStore,
		PlanStore:             e.planStore,
		EventLog:              e.eventLog,
		Middlewares:           e.cfg.Middlewares,
		WorkspaceRoot:         e.cfg.WorkspaceRoot,
		ApprovalMode:          approvalMode,
		EmitThinking:          emitThinking,
		AutoCompressThreshold: e.cfg.AutoCompressThreshold,
		CompressKeepTurns:     e.cfg.CompressKeepTurns,
		FilterHistoryTools:    e.cfg.FilterHistoryTools,
	})

	e.activeTurns[sessionID] = runner
	e.turnsMu.Unlock()

	// Start turn
	stream, err := runner.Run(ctx, session, message)
	if err != nil {
		e.turnsMu.Lock()
		delete(e.activeTurns, sessionID)
		e.turnsMu.Unlock()
		return nil, err
	}

	// Wrap stream to cleanup on close
	return &cleanupEventStream{
		EventStream: stream,
		onClose: func() {
			e.turnsMu.Lock()
			delete(e.activeTurns, sessionID)
			e.turnsMu.Unlock()
		},
	}, nil
}

// Resume continues from a pending approval.
func (e *Engine) Resume(ctx context.Context, sessionID string, decision api.Decision) (api.EventStream, error) {
	// Check for existing active turn
	e.turnsMu.Lock()
	if _, exists := e.activeTurns[sessionID]; exists {
		e.turnsMu.Unlock()
		return nil, fmt.Errorf("%s: %s", api.ErrTurnInProgress, sessionID)
	}

	// Load session with retry for pending state sync
	var session *api.Session
	var err error
	for i := 0; i < 3; i++ {
		session, err = e.sessionStore.Get(ctx, sessionID)
		if err != nil {
			e.turnsMu.Unlock()
			if err == store.ErrNotFound {
				return nil, fmt.Errorf("%s: %s", api.ErrInvalidSession, sessionID)
			}
			return nil, err
		}
		if session.Pending != nil {
			break // Got pending, proceed
		}
		// Wait a bit for pending to be written (timing issue workaround)
		if i < 2 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Verify pending approval exists
	if session.Pending == nil {
		e.turnsMu.Unlock()
		return nil, fmt.Errorf("%s: %s", api.ErrNoPendingApproval, sessionID)
	}

	approvalMode := api.ModeAuto
	emitThinking := false
	if session.Metadata != nil {
		if v := session.Metadata["approval_mode"]; v != "" {
			approvalMode = api.ApprovalMode(v)
		}
		if session.Metadata["emit_thinking"] == "true" {
			emitThinking = true
		}
	}

	// Create turn runner
	runner := NewTurnRunner(TurnRunnerConfig{
		LLM:                   e.cfg.LLM,
		Tools:                 e.cfg.Tools,
		Policy:                e.cfg.Policy,
		SessionStore:          e.sessionStore,
		PlanStore:             e.planStore,
		EventLog:              e.eventLog,
		Middlewares:           e.cfg.Middlewares,
		WorkspaceRoot:         e.cfg.WorkspaceRoot,
		ApprovalMode:          approvalMode,
		EmitThinking:          emitThinking,
		AutoCompressThreshold: e.cfg.AutoCompressThreshold,
		CompressKeepTurns:     e.cfg.CompressKeepTurns,
		FilterHistoryTools:    e.cfg.FilterHistoryTools,
	})

	e.activeTurns[sessionID] = runner
	e.turnsMu.Unlock()

	// Resume turn
	stream, err := runner.Resume(ctx, session, decision)
	if err != nil {
		e.turnsMu.Lock()
		delete(e.activeTurns, sessionID)
		e.turnsMu.Unlock()
		return nil, err
	}

	return &cleanupEventStream{
		EventStream: stream,
		onClose: func() {
			e.turnsMu.Lock()
			delete(e.activeTurns, sessionID)
			e.turnsMu.Unlock()
		},
	}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// cleanupEventStream wraps EventStream to run cleanup on close.
type cleanupEventStream struct {
	api.EventStream
	onClose func()
	closed  bool
	mu      sync.Mutex
}

func (s *cleanupEventStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	err := s.EventStream.Close()
	if s.onClose != nil {
		s.onClose()
	}
	return err
}
