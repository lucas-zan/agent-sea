package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"AgentEngine/cmd/ui"
	"AgentEngine/pkg/engine/api"
)

type approvalState struct {
	autoApproveAll bool
}

func runTurnWithApprovals(ctx context.Context, eng api.Engine, sessionID, message string, approver *ui.CLIApprover, a *approvalState) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := eng.Send(ctx, sessionID, message)
	if err != nil {
		return err
	}

	for {
		pending, err := consumeEventStream(ctx, stream, cancel)
		if err != nil {
			stream.Close()
			return err
		}
		if pending == nil {
			stream.Close()
			return nil
		}

		var decision api.Decision
		if a != nil && a.autoApproveAll {
			decision = api.Decision{Kind: api.DecisionApprove, RequestID: pending.RequestID, ToolCallID: pending.ToolCallID}
		} else {
			d, autoAll, err := approver.RequestApproval(ctx, *pending)
			if err != nil {
				stream.Close()
				return err
			}
			decision = d
			if a != nil && autoAll {
				a.autoApproveAll = true
			}
		}

		// Close the current stream and resume.
		_ = stream.Close()
		stream, err = eng.Resume(ctx, sessionID, decision)
		if err != nil {
			errStr := err.Error()
			// In auto-approve mode, if no pending approval exists or turn completed,
			// the turn may have already finished processing. This is OK.
			if strings.Contains(errStr, "no_pending_approval") {
				return nil
			}
			// If turn_in_progress but pending approval exists, try clearing and retrying
			// This handles the case where the session state is inconsistent
			if strings.Contains(errStr, "turn_in_progress") && strings.Contains(errStr, "pending") {
				// The approval flow got confused - just return nil to let user continue
				return nil
			}
			return err
		}
	}
}

func consumeEventStream(ctx context.Context, stream api.EventStream, cancel context.CancelFunc) (*api.ApprovalPayload, error) {
	// Start input monitor for cancellation (switch to raw mode)
	cleanup := monitorCancellation(ctx, cancel)
	defer cleanup()

	stopSpinner, spinnerDone := ui.StartLoading("Thinking...")
	defer func() {
		select {
		case <-stopSpinner:
		default:
			close(stopSpinner)
		}
		<-spinnerDone
	}()

	prefixPrinted := false
	firstEvent := true
	toolArgBuffer := "" // Buffer for scrolling tool argument display

	for {
		e, err := stream.Recv(ctx)
		if err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, err
		}

		if firstEvent {
			close(stopSpinner)
			<-spinnerDone
			firstEvent = false
		}

		switch e.Type {
		case api.EventThinking:
			// Keep thinking output lightweight to avoid UI spam.
			if e.Thinking != nil && strings.TrimSpace(e.Thinking.Message) != "" {
				ui.Printf("\n🤔 %s\n", e.Thinking.Message)
			}

		case api.EventDelta:
			if e.Delta == nil || e.Delta.Text == "" {
				continue
			}
			// Style based on delta source
			switch e.Delta.Source {
			case api.DeltaToolArg:
				// Scrolling gray display: append to buffer, show only last ~60 chars
				toolArgBuffer += e.Delta.Text
				// Keep only last 80 chars for display (scrolling effect)
				display := toolArgBuffer
				if len(display) > 80 {
					display = "..." + display[len(display)-77:]
				}
				// Use carriage return to overwrite the same line
				ui.Printf("\r\033[90m   %s\033[0m\033[K", display)
			default:
				// Clear tool arg display line if any
				if toolArgBuffer != "" {
					ui.Print("\r\033[K") // Clear the gray line
					toolArgBuffer = ""
				}
				// Normal text
				if !prefixPrinted {
					ui.Print("\n🤖 Agent: ")
					prefixPrinted = true
				}
				ui.Print(e.Delta.Text)
			}

		case api.EventToolCall:
			if e.ToolCall == nil {
				continue
			}
			// Clear tool arg display line
			if toolArgBuffer != "" {
				ui.Print("\r\033[K") // Clear the gray line
				toolArgBuffer = ""
			}
			// Show tool call with simple visual indicator
			ui.Printf("\n\n🔧 tool_call %s\n", e.ToolCall.ToolName)

		case api.EventToolResult:
			if e.ToolResult == nil {
				continue
			}
			ui.Printf("\n🔧 tool_result %s (%s)\n", e.ToolResult.ToolName, e.ToolResult.Result.Status)
			if e.ToolResult.Result.Status == "error" && e.ToolResult.Result.Error != "" {
				ui.Printf("Error: %s\n", e.ToolResult.Result.Error)
			} else if e.ToolResult.Result.Content != "" {
				ui.Print(e.ToolResult.Result.Content)
				if !strings.HasSuffix(e.ToolResult.Result.Content, "\n") {
					ui.Print("\n")
				}
			}

		case api.EventPlan:
			if e.Plan == nil {
				continue
			}
			renderPlan(*e.Plan)

		case api.EventApproval:
			if e.Approval == nil {
				return nil, fmt.Errorf("approval event missing payload")
			}
			// UI uses approval payload for prompt; engine waits for Resume().
			return e.Approval, nil

		case api.EventError:
			if e.Error != nil {
				return nil, fmt.Errorf("%s: %s", e.Error.Code, e.Error.Message)
			}
			return nil, fmt.Errorf("unknown error")

		case api.EventDone:
			if prefixPrinted {
				ui.Print("\n")
			}
			return nil, nil
		}
	}
}

func renderPlan(plan api.PlanPayload) {
	if len(plan.Items) == 0 {
		return
	}
	total := len(plan.Items)
	done := 0
	for _, it := range plan.Items {
		if it.Status == api.PlanDone {
			done++
		}
	}

	ui.Printf("\n\n🗂️  plan %s (%d/%d done)\n", plan.PlanID, done, total)
	for _, it := range plan.Items {
		ui.Printf("  - [%s] %d. %s\n", it.Status, it.ID, it.Text)
	}
	ui.Print("\n")
}
