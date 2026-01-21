// Package runtime provides the core execution engine.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/policy"
	"AgentEngine/pkg/engine/store"
	"AgentEngine/pkg/engine/tools"
	"AgentEngine/pkg/logger"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Turn State Machine
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TurnState represents the current state of a turn.
type TurnState string

const (
	StateIdle            TurnState = "idle"
	StateRunning         TurnState = "running"
	StateToolProposed    TurnState = "tool_proposed"
	StateWaitingApproval TurnState = "waiting_approval"
	StateExecutingTool   TurnState = "executing_tool"
	StateCompleted       TurnState = "completed"
	StateError           TurnState = "error"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Dependencies
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LLM is the interface for language model interactions.
type LLM interface {
	Stream(ctx context.Context, req LLMRequest) (LLMStream, error)
}

// LLMRequest represents a request to the LLM.
type LLMRequest struct {
	Messages  []api.LLMMessage
	Tools     []api.ToolSchema
	MaxTokens int
}

// LLMStream is a streaming response from the LLM.
type LLMStream interface {
	Recv(ctx context.Context) (LLMChunk, error)
	Close() error
}

// LLMChunk is a chunk of streaming LLM response.
type LLMChunk struct {
	Delta        string           // Text content delta
	ToolArgDelta string           // Tool argument delta (for streaming display)
	ToolCall     *api.LLMToolCall // Complete tool call (when finish_reason=tool_calls)
	FinishReason string
}

// Tool is the unified executable tool interface used by the runtime.
type Tool = tools.Tool

// ToolRegistry provides tool lookup.
type ToolRegistry interface {
	Get(name string) (Tool, bool)
	All() []Tool
}

// Middleware processes turns.
type Middleware interface {
	Name() string
	BeforeTurn(ctx context.Context, state *api.State) error
	OnEvent(ctx context.Context, state *api.State, e api.Event) error
	AfterTurn(ctx context.Context, state *api.State, summary api.TurnSummary) error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TurnRunner
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TurnRunnerConfig holds turn runner dependencies.
type TurnRunnerConfig struct {
	LLM          LLM
	Tools        ToolRegistry
	Policy       policy.Policy
	SessionStore store.SessionStore
	PlanStore    store.PlanStore
	EventLog     store.EventLog
	Middlewares  []Middleware

	WorkspaceRoot string
	ApprovalMode  api.ApprovalMode
	EmitThinking  bool

	// Compression settings
	AutoCompressThreshold int // 0 = disabled, otherwise auto-compress when messages >= this
	CompressKeepTurns     int // Number of turns to keep (default: 3)

	// Message filtering: if true, filter out historical tool_calls/tool messages
	// before sending to LLM (keep only current turn's tool interactions)
	FilterHistoryTools bool
}

// TurnRunner executes a single turn of conversation.
type TurnRunner struct {
	cfg TurnRunnerConfig

	// Turn state
	state     TurnState
	session   *api.Session
	turnID    string
	seq       int64
	events    *store.ChannelEventStream
	startedAt time.Time

	// Tracking
	toolCalls     []api.ToolCallRef
	approvals     []api.ApprovalRef
	assistantText string
	turnOutcome   api.TurnOutcome
	turnError     *api.ErrorPayload
	hookState     *api.State

	mu sync.Mutex
}

// NewTurnRunner creates a new turn runner.
func NewTurnRunner(cfg TurnRunnerConfig) *TurnRunner {
	return &TurnRunner{
		cfg:    cfg,
		state:  StateIdle,
		events: store.NewChannelEventStream(100),
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Public API
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Run starts a new turn with a user message.
func (r *TurnRunner) Run(ctx context.Context, session *api.Session, message string) (api.EventStream, error) {
	r.mu.Lock()
	if r.state != StateIdle {
		r.mu.Unlock()
		return nil, fmt.Errorf("%s: turn already in progress", api.ErrTurnInProgress)
	}
	r.state = StateRunning
	r.session = session
	r.turnID = generateTurnID()
	r.seq = 0
	r.startedAt = time.Now()
	r.mu.Unlock()

	// Run the turn in background
	go r.runTurn(ctx, message)

	return r.events, nil
}

// Resume continues a turn from pending approval.
func (r *TurnRunner) Resume(ctx context.Context, session *api.Session, decision api.Decision) (api.EventStream, error) {
	r.mu.Lock()
	if session.Pending == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("%s: no pending approval", api.ErrNoPendingApproval)
	}

	// Validate decision matches pending
	if decision.RequestID != session.Pending.RequestID {
		r.mu.Unlock()
		return nil, fmt.Errorf("%s: request ID mismatch", api.ErrApprovalMismatch)
	}
	if decision.ToolCallID != "" && decision.ToolCallID != session.Pending.ToolCall.ToolCallID {
		r.mu.Unlock()
		return nil, fmt.Errorf("%s: tool call ID mismatch", api.ErrApprovalMismatch)
	}

	r.state = StateExecutingTool
	r.session = session
	r.turnID = session.Pending.TurnID // Continue the same turn
	r.mu.Unlock()

	// Reset event stream for resume
	r.events = store.NewChannelEventStream(100)

	// Run resume in background
	go r.resumeTurn(ctx, decision)

	return r.events, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Internal Execution
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (r *TurnRunner) runTurn(ctx context.Context, message string) {
	defer r.events.Close()
	defer r.finalize(ctx)

	// Emit thinking if enabled
	if r.cfg.EmitThinking {
		r.emit(ctx, api.Event{
			Type:     api.EventThinking,
			Thinking: &api.ThinkingPayload{Message: "Analyzing request..."},
		})
	}

	// Emit plan snapshot if exists
	if err := r.emitPlanSnapshot(ctx, ""); err != nil {
		r.emitError(ctx, api.ErrStoreError, err.Error())
		return
	}

	// Append user message
	userMsg := api.LLMMessage{Role: "user", Content: message}
	r.session.Messages = append(r.session.Messages, userMsg)

	// Auto-compress if threshold exceeded
	if r.cfg.AutoCompressThreshold > 0 && len(r.session.Messages) >= r.cfg.AutoCompressThreshold {
		keepTurns := r.cfg.CompressKeepTurns
		if keepTurns <= 0 {
			keepTurns = 3
		}
		logger.Info("Compress", "Auto-compressing session", map[string]interface{}{
			"threshold":     r.cfg.AutoCompressThreshold,
			"message_count": len(r.session.Messages),
			"keep_turns":    keepTurns,
		})
		r.emit(ctx, api.Event{
			Type:     api.EventThinking,
			Thinking: &api.ThinkingPayload{Message: "🔄 Auto-compressing conversation history..."},
		})
		if err := CompressHistory(ctx, r.cfg.LLM, r.session, CompressConfig{KeepTurns: keepTurns}); err != nil {
			logger.Warn("Compress", "Auto-compression failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Save session
	if err := r.saveSession(ctx); err != nil {
		r.emitError(ctx, api.ErrStoreError, err.Error())
		return
	}

	state := &api.State{
		SessionID:   r.session.SessionID,
		TurnID:      r.turnID,
		ActiveSkill: r.session.ActiveSkill,
		Messages:    append([]api.LLMMessage(nil), r.session.Messages...),
		Metadata:    make(map[string]any),
	}
	// Inject session summary for middleware to use
	if r.session.Summary != "" {
		state.Metadata["session_summary"] = r.session.Summary
	}
	r.hookState = state

	// Run agent loop
	outcome, err := r.agentLoop(ctx, state)
	if err != nil {
		if errorsIsContextCanceled(err) {
			r.emitDone(ctx, "canceled")
			return
		}
		r.emitError(ctx, api.ErrToolExecuteFailed, err.Error())
		return
	}

	if outcome == loopOutcomeSuspended {
		return
	}
	r.emitDone(ctx, "completed")
}

func (r *TurnRunner) resumeTurn(ctx context.Context, decision api.Decision) {
	defer r.events.Close()
	defer r.finalize(ctx)

	// Emit plan snapshot if exists (UI can render progress panel immediately).
	if err := r.emitPlanSnapshot(ctx, ""); err != nil {
		r.emitError(ctx, api.ErrStoreError, err.Error())
		return
	}

	pending := r.session.Pending

	if decision.Kind == api.DecisionReject {
		// Clear pending and emit done
		r.session.Pending = nil
		if err := r.saveSession(ctx); err != nil {
			r.emitError(ctx, api.ErrStoreError, err.Error())
			return
		}
		r.emitDone(ctx, "rejected")
		return
	}

	// Get tool and args
	args := pending.ToolCall.Args
	if decision.Kind == api.DecisionModify && decision.ModifiedArgs != nil {
		args = decision.ModifiedArgs
	}
	execArgs := r.prepareExecArgs(pending.ToolCall.ToolName, args)

	// Build state and run middlewares (to enforce allowed-tools and inject system prompt).
	state := &api.State{
		SessionID:   r.session.SessionID,
		TurnID:      r.turnID,
		ActiveSkill: r.session.ActiveSkill,
		Messages:    append([]api.LLMMessage(nil), r.session.Messages...),
		Metadata:    make(map[string]any),
	}
	r.hookState = state
	if err := r.refreshState(ctx, state); err != nil {
		r.emitError(ctx, api.ErrStoreError, err.Error())
		return
	}

	// Execute tool
	tool, ok := r.cfg.Tools.Get(pending.ToolCall.ToolName)
	if !ok {
		r.emitError(ctx, api.ErrToolNotFound, pending.ToolCall.ToolName)
		return
	}

	// Validate before execution (modified args may be denied or require re-approval).
	pctx := api.PolicyContext{
		SessionID:      r.session.SessionID,
		TurnID:         r.turnID,
		ApprovalMode:   r.cfg.ApprovalMode,
		WorkspaceRoot:  r.cfg.WorkspaceRoot,
		AllowedTools:   getAllowedToolsFromState(state),
		ToolCallOrigin: api.OriginModel,
	}

	if err := r.cfg.Policy.Validate(ctx, pctx, tool, execArgs); err != nil {
		r.emit(ctx, api.Event{
			Type: api.EventToolResult,
			ToolResult: &api.ToolResultPayload{
				ToolCallID: pending.ToolCall.ToolCallID,
				ToolName:   pending.ToolCall.ToolName,
				Result:     api.ToolResult{Status: "error", Error: err.Error()},
			},
		})
		r.session.Pending = nil
		_ = r.saveSession(ctx)
		r.emitDone(ctx, "completed")
		return
	}

	// Note: We don't re-check NeedApproval here because the user has already
	// approved this tool call. Re-checking would cause an infinite loop since
	// tools like 'shell' always require approval in auto mode.

	result, err := tool.Execute(ctx, execArgs)
	if err != nil {
		result = api.ToolResult{Status: "error", Error: err.Error()}
	}

	// Apply engine-side effects for certain system tools.
	if pending.ToolCall.ToolName == "activate_skill" && result.Status == "success" {
		if name, ok := args["name"].(string); ok && name != "" {
			r.session.ActiveSkill = name
		}
	}

	// Emit tool result
	r.emit(ctx, api.Event{
		Type: api.EventToolResult,
		ToolResult: &api.ToolResultPayload{
			ToolCallID: pending.ToolCall.ToolCallID,
			ToolName:   pending.ToolCall.ToolName,
			Result:     result,
		},
	})

	// Append tool message
	toolMsg := api.LLMMessage{
		Role:       "tool",
		Content:    result.Content,
		ToolCallID: pending.ToolCall.ToolCallID,
	}
	r.session.Messages = append(r.session.Messages, toolMsg)

	// Clear pending
	r.session.Pending = nil
	if err := r.saveSession(ctx); err != nil {
		r.emitError(ctx, api.ErrStoreError, err.Error())
		return
	}

	// Check for plan update
	if pending.ToolCall.ToolName == "write_todos" {
		if err := r.emitPlanSnapshot(ctx, pending.ToolCall.ToolCallID); err != nil {
			r.emitError(ctx, api.ErrStoreError, err.Error())
			return
		}
	}

	// Continue agent loop
	outcome, err := r.agentLoop(ctx, state)
	if err != nil {
		if errorsIsContextCanceled(err) {
			r.emitDone(ctx, "canceled")
			return
		}
		r.emitError(ctx, api.ErrToolExecuteFailed, err.Error())
		return
	}

	if outcome == loopOutcomeSuspended {
		return
	}
	r.emitDone(ctx, "completed")
}

type loopOutcome int

const (
	loopOutcomeCompleted loopOutcome = iota
	loopOutcomeSuspended
)

func (r *TurnRunner) agentLoop(ctx context.Context, state *api.State) (loopOutcome, error) {
	for {
		select {
		case <-ctx.Done():
			return loopOutcomeCompleted, ctx.Err()
		default:
		}

		// Refresh turn state (skill/memory/plan injection, allowed-tools).
		if err := r.refreshState(ctx, state); err != nil {
			return loopOutcomeCompleted, err
		}

		// Build policy context
		pctx := api.PolicyContext{
			SessionID:      r.session.SessionID,
			TurnID:         r.turnID,
			ApprovalMode:   r.cfg.ApprovalMode,
			WorkspaceRoot:  r.cfg.WorkspaceRoot,
			AllowedTools:   getAllowedToolsFromState(state),
			ToolCallOrigin: api.OriginModel,
		}

		// Get visible tools
		allTools := r.cfg.Tools.All()
		policyTools := make([]policy.Tool, len(allTools))
		for i, t := range allTools {
			policyTools[i] = t
		}
		visibleTools := r.cfg.Policy.Filter(ctx, pctx, policyTools)

		// Convert to schemas
		var toolSchemas []api.ToolSchema
		for _, pt := range visibleTools {
			if t, ok := r.cfg.Tools.Get(pt.Name()); ok {
				toolSchemas = append(toolSchemas, t.Schema())
			}
		}

		// Build LLM request: prepend a system prompt for this turn (not persisted).
		messages := state.Messages
		if r.cfg.FilterHistoryTools {
			messages = filterHistoryToolMessages(messages)
		}
		req := LLMRequest{
			Messages: buildRequestMessages(state.SystemPrompt, messages),
			Tools:    toolSchemas,
		}

		// Stream LLM response
		stream, err := r.cfg.LLM.Stream(ctx, req)
		if err != nil {
			return loopOutcomeCompleted, fmt.Errorf("LLM stream error: %w", err)
		}

		var assistantContent string
		var toolCalls []api.LLMToolCall

		for {
			chunk, err := stream.Recv(ctx)
			if err != nil {
				stream.Close()
				if err == io.EOF {
					break
				}
				return loopOutcomeCompleted, fmt.Errorf("LLM recv error: %w", err)
			}

			if chunk.Delta != "" {
				assistantContent += chunk.Delta
				r.emit(ctx, api.Event{
					Type:  api.EventDelta,
					Delta: &api.DeltaPayload{Text: chunk.Delta, Source: api.DeltaText},
				})
			}

			// Emit tool argument delta for streaming display (gray text in UI)
			if chunk.ToolArgDelta != "" {
				r.emit(ctx, api.Event{
					Type:  api.EventDelta,
					Delta: &api.DeltaPayload{Text: chunk.ToolArgDelta, Source: api.DeltaToolArg},
				})
			}

			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
			}

			if chunk.FinishReason != "" {
				break
			}
		}
		stream.Close()

		// No tool calls - turn complete
		if len(toolCalls) == 0 {
			// Save assistant message
			if assistantContent != "" {
				r.session.Messages = append(r.session.Messages, api.LLMMessage{
					Role:    "assistant",
					Content: assistantContent,
				})
				if err := r.saveSession(ctx); err != nil {
					return loopOutcomeCompleted, err
				}
			}
			r.assistantText = assistantContent
			return loopOutcomeCompleted, nil
		}

		// Before processing tool calls, save the assistant message with tool_calls
		// OpenAI API requires: user → assistant (with tool_calls) → tool results
		assistantMsg := api.LLMMessage{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
		}
		r.session.Messages = append(r.session.Messages, assistantMsg)
		if err := r.saveSession(ctx); err != nil {
			return loopOutcomeCompleted, err
		}

		// Process tool calls
		for _, tc := range toolCalls {
			// Parse args (must be valid JSON).
			var args api.Args
			if strings.TrimSpace(tc.Args) != "" {
				if err := json.Unmarshal([]byte(tc.Args), &args); err != nil {
					r.emit(ctx, api.Event{
						Type: api.EventToolResult,
						ToolResult: &api.ToolResultPayload{
							ToolCallID: tc.ID,
							ToolName:   tc.Name,
							Result:     api.ToolResult{Status: "error", Error: fmt.Sprintf("%s: invalid JSON args: %v", api.ErrToolArgsInvalid, err)},
						},
					})
					continue
				}
			} else {
				args = make(api.Args)
			}

			toolCall := api.ToolCallPayload{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Args:       args,
			}

			// Check policy
			tool, ok := r.cfg.Tools.Get(tc.Name)
			if !ok {
				r.emit(ctx, api.Event{
					Type: api.EventToolResult,
					ToolResult: &api.ToolResultPayload{
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
						Result:     api.ToolResult{Status: "error", Error: "tool not found"},
					},
				})
				continue
			}

			execArgs := r.prepareExecArgs(tc.Name, args)
			needApproval := r.cfg.Policy.NeedApproval(ctx, pctx, tool, execArgs)
			toolCall.NeedApproval = needApproval

			// Best-effort preview for approval UI.
			var preview *api.Preview
			if needApproval {
				if p, ok := tool.(tools.Previewer); ok {
					if v, err := p.Preview(ctx, execArgs); err == nil {
						preview = v
					}
				}
			}
			toolCall.Preview = preview

			// Emit tool call (for UI/log grouping).
			r.emit(ctx, api.Event{
				Type:     api.EventToolCall,
				ToolCall: &toolCall,
			})

			// Validate
			if err := r.cfg.Policy.Validate(ctx, pctx, tool, execArgs); err != nil {
				r.emit(ctx, api.Event{
					Type: api.EventToolResult,
					ToolResult: &api.ToolResultPayload{
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
						Result:     api.ToolResult{Status: "error", Error: err.Error()},
					},
				})
				continue
			}

			// Check approval
			if needApproval {
				requestID := generateRequestID()
				// Emit approval and suspend
				r.emit(ctx, api.Event{
					Type: api.EventApproval,
					Approval: &api.ApprovalPayload{
						RequestID:  requestID,
						ToolCallID: tc.ID,
						ToolCall:   toolCall,
						Mode:       r.cfg.ApprovalMode,
					},
				})

				// Save pending state
				r.session.Pending = &api.PendingApproval{
					TurnID:    r.turnID,
					RequestID: requestID,
					ToolCall:  toolCall,
					Preview:   preview,
					CreatedAt: time.Now(),
				}
				if err := r.saveSession(ctx); err != nil {
					return loopOutcomeCompleted, err
				}

				return loopOutcomeSuspended, nil // Suspend - wait for Resume
			}

			// Execute tool
			result, err := tool.Execute(ctx, execArgs)
			if err != nil {
				result = api.ToolResult{Status: "error", Error: err.Error()}
			}

			// Apply engine-side effects for certain system tools.
			if tc.Name == "activate_skill" && result.Status == "success" {
				if name, ok := args["name"].(string); ok && name != "" {
					r.session.ActiveSkill = name
				}
			}

			r.emit(ctx, api.Event{
				Type: api.EventToolResult,
				ToolResult: &api.ToolResultPayload{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Result:     result,
				},
			})

			// Add to messages
			r.session.Messages = append(r.session.Messages, api.LLMMessage{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: tc.ID,
			})
			if err := r.saveSession(ctx); err != nil {
				return loopOutcomeCompleted, err
			}

			// Check for plan update
			if tc.Name == "write_todos" {
				_ = r.emitPlanSnapshot(ctx, tc.ID)
			}
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (r *TurnRunner) emit(ctx context.Context, e api.Event) {
	r.mu.Lock()
	r.seq++
	e.Version = 1
	e.SessionID = r.session.SessionID
	e.TurnID = r.turnID
	e.Seq = r.seq
	e.Ts = time.Now()
	r.mu.Unlock()

	r.events.Send(e)

	// Log event
	if r.cfg.EventLog != nil {
		r.cfg.EventLog.Append(context.WithoutCancel(ctx), e)
	}

	// Track tool/approval refs for AfterTurn summaries.
	switch e.Type {
	case api.EventToolCall:
		if e.ToolCall != nil {
			r.toolCalls = append(r.toolCalls, api.ToolCallRef{ToolCallID: e.ToolCall.ToolCallID, ToolName: e.ToolCall.ToolName})
		}
	case api.EventApproval:
		if e.Approval != nil {
			r.approvals = append(r.approvals, api.ApprovalRef{RequestID: e.Approval.RequestID, ToolCallID: e.Approval.ToolCallID})
		}
	}

	// Middleware event hook (best-effort, must not block the main loop).
	for _, mw := range r.cfg.Middlewares {
		_ = mw.OnEvent(ctx, r.hookState, e)
	}
}

func (r *TurnRunner) emitError(ctx context.Context, code, message string) {
	r.turnOutcome = api.TurnError
	r.turnError = &api.ErrorPayload{Code: code, Message: message}
	r.emit(ctx, api.Event{
		Type:  api.EventError,
		Error: &api.ErrorPayload{Code: code, Message: message},
	})
	r.emitDone(ctx, "error")
}

func (r *TurnRunner) emitDone(ctx context.Context, reason string) {
	switch reason {
	case "canceled":
		r.turnOutcome = api.TurnCanceled
	case "error":
		r.turnOutcome = api.TurnError
	default:
		r.turnOutcome = api.TurnDone
	}
	r.emit(ctx, api.Event{
		Type: api.EventDone,
		Done: &api.DonePayload{Reason: reason},
	})
	r.mu.Lock()
	r.state = StateCompleted
	r.mu.Unlock()
}

func (r *TurnRunner) emitPlanSnapshot(ctx context.Context, toolCallID string) error {
	planID := "plan_" + r.session.SessionID
	plan, err := r.cfg.PlanStore.Get(ctx, planID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil // No plan exists
		}
		return err
	}

	out := *plan
	if toolCallID != "" {
		out.ToolCallID = toolCallID
	}

	r.emit(ctx, api.Event{
		Type: api.EventPlan,
		Plan: &out,
	})
	return nil
}

func generateTurnID() string {
	return fmt.Sprintf("turn_%d", time.Now().UnixMilli())
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func buildRequestMessages(systemPrompt string, messages []api.LLMMessage) []api.LLMMessage {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return append([]api.LLMMessage(nil), messages...)
	}
	out := make([]api.LLMMessage, 0, len(messages)+1)
	out = append(out, api.LLMMessage{Role: "system", Content: systemPrompt})
	out = append(out, messages...)
	return out
}

// filterHistoryToolMessages filters out historical tool_calls and tool messages,
// keeping only the last turn's tool interactions. This reduces context size
// while preserving the current turn's tool state for models that require it.
func filterHistoryToolMessages(messages []api.LLMMessage) []api.LLMMessage {
	if len(messages) == 0 {
		return messages
	}

	// Find the last user message (start of current turn)
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx < 0 {
		// No user message found, keep all
		return messages
	}

	// Build filtered messages:
	// - Keep all user messages and assistant text (no tool_calls) from history
	// - Keep everything from the last user message onward (current turn)
	var result []api.LLMMessage

	// Process history (before last user message)
	for i := 0; i < lastUserIdx; i++ {
		m := messages[i]
		switch m.Role {
		case "user":
			result = append(result, m)
		case "assistant":
			// Keep assistant messages, but strip tool_calls from history
			if len(m.ToolCalls) > 0 {
				// Convert to text-only if there was content, otherwise skip
				if m.Content != "" {
					result = append(result, api.LLMMessage{
						Role:    "assistant",
						Content: m.Content,
					})
				}
				// Skip the tool_calls entirely for historical messages
			} else {
				result = append(result, m)
			}
		case "tool":
			// Skip historical tool messages
		}
	}

	// Keep everything from current turn (lastUserIdx onward)
	result = append(result, messages[lastUserIdx:]...)

	return result
}

func getAllowedToolsFromState(state *api.State) []string {
	if state == nil || state.Metadata == nil {
		return nil
	}
	raw, ok := state.Metadata["allowed_tools"]
	if !ok {
		return nil
	}
	if list, ok := raw.([]string); ok {
		return append([]string(nil), list...)
	}
	if ifaceList, ok := raw.([]any); ok {
		out := make([]string, 0, len(ifaceList))
		for _, v := range ifaceList {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func errorsIsContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (r *TurnRunner) prepareExecArgs(toolName string, args api.Args) api.Args {
	// System tools must always operate on the current session, never on a model-supplied session id.
	// Keep args stable for UI/events by injecting into the execution args only.
	switch toolName {
	case "read_todos", "write_todos":
		out := make(api.Args, len(args)+1)
		for k, v := range args {
			out[k] = v
		}
		out["session_id"] = r.session.SessionID
		return out
	case "run_skill_script":
		// Inject active skill for validation and path resolution.
		out := make(api.Args, len(args)+1)
		for k, v := range args {
			out[k] = v
		}
		out["_active_skill"] = r.session.ActiveSkill
		return out
	default:
		return args
	}
}

func (r *TurnRunner) refreshState(ctx context.Context, state *api.State) error {
	if state == nil {
		return nil
	}
	state.ActiveSkill = r.session.ActiveSkill
	state.Messages = append([]api.LLMMessage(nil), r.session.Messages...)
	state.SystemPrompt = ""
	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	} else {
		for k := range state.Metadata {
			delete(state.Metadata, k)
		}
	}

	for _, mw := range r.cfg.Middlewares {
		if err := mw.BeforeTurn(ctx, state); err != nil {
			return fmt.Errorf("middleware %s: %v", mw.Name(), err)
		}
	}
	return nil
}

func (r *TurnRunner) finalize(ctx context.Context) {
	// Suspended turns (waiting approval) must not be finalized.
	if r.turnOutcome == "" {
		return
	}

	summary := api.TurnSummary{
		SessionID:     r.session.SessionID,
		TurnID:        r.turnID,
		Outcome:       r.turnOutcome,
		AssistantText: r.assistantText,
		ToolCalls:     append([]api.ToolCallRef(nil), r.toolCalls...),
		Approvals:     append([]api.ApprovalRef(nil), r.approvals...),
		Error:         r.turnError,
		StartedAt:     r.startedAt,
		FinishedAt:    time.Now(),
	}

	// AfterTurn runs in reverse order (as specified by mw.Chain), but the runtime stores middlewares as a slice.
	for i := len(r.cfg.Middlewares) - 1; i >= 0; i-- {
		_ = r.cfg.Middlewares[i].AfterTurn(ctx, r.hookState, summary)
	}

	// Prevent double-finalize.
	r.turnOutcome = ""
}

func (r *TurnRunner) saveSession(ctx context.Context) error {
	r.session.UpdatedAt = time.Now()
	return r.cfg.SessionStore.Put(ctx, r.session.SessionID, r.session)
}
