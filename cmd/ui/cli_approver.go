package ui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"AgentEngine/pkg/engine/api"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// CLIApprover implements ApprovalHandler for terminal interaction
type CLIApprover struct {
	// Reader for input (defaults to os.Stdin)
	Reader *bufio.Reader
}

// NewCLIApprover creates a new CLI approver
func NewCLIApprover() *CLIApprover {
	return &CLIApprover{
		Reader: bufio.NewReader(os.Stdin),
	}
}

// RequestApproval prompts the user with an interactive approval UI
func (c *CLIApprover) RequestApproval(ctx context.Context, req api.ApprovalPayload) (api.Decision, bool, error) {
	// Print approval panel
	fmt.Println()
	fmt.Println("\033[33m╭──────────────────────────────────────────────────────────╮\033[0m")
	fmt.Println("\033[33m│\033[0m  \033[1;33m⚠️  Tool Action Requires Approval\033[0m                        \033[33m│\033[0m")
	fmt.Println("\033[33m╰──────────────────────────────────────────────────────────╯\033[0m")
	fmt.Println()

	// Show preview if available
	if req.ToolCall.Preview != nil {
		fmt.Printf("\033[1mPreview:\033[0m %s\n", req.ToolCall.Preview.Summary)
		if req.ToolCall.Preview.RiskHint != "" {
			fmt.Printf("\033[1mRisk:\033[0m %s\n", req.ToolCall.Preview.RiskHint)
		}
		if len(req.ToolCall.Preview.Affected) > 0 {
			fmt.Printf("\033[1mAffected:\033[0m %s\n", strings.Join(req.ToolCall.Preview.Affected, ", "))
		}
		if req.ToolCall.Preview.Content != "" {
			fmt.Println()
			fmt.Println(req.ToolCall.Preview.Content)
		}
	} else {
		fmt.Printf("\033[1mTool:\033[0m %s\n", req.ToolCall.ToolName)
		if len(req.ToolCall.Args) > 0 {
			fmt.Println("\033[1mArguments:\033[0m")
			for k, v := range req.ToolCall.Args {
				vStr := fmt.Sprintf("%v", v)
				if len(vStr) > 100 {
					vStr = vStr[:100] + "..."
				}
				fmt.Printf("  %s: %s\n", k, vStr)
			}
		}
	}

	fmt.Println()

	// Try interactive mode first
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return c.interactiveApproval(req)
	}

	// Fallback to simple prompt
	return c.simpleApproval(req)
}

// interactiveApproval uses bubbletea for selection
func (c *CLIApprover) interactiveApproval(req api.ApprovalPayload) (api.Decision, bool, error) {
	model := initialApprovalModel(req)
	p := tea.NewProgram(model)

	// Run the program
	finalModel, err := p.Run()
	if err != nil {
		return c.simpleApproval(req)
	}

	m, ok := finalModel.(approvalModel)
	if !ok || m.cancelled {
		return api.Decision{
			Kind:       api.DecisionReject,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	}

	return c.makeDecision(req, m.selected)
}

// approvalModel is the bubbletea model for the approval prompt
type approvalModel struct {
	req       api.ApprovalPayload
	options   []string
	selected  int
	cancelled bool
	chosen    bool
}

func initialApprovalModel(req api.ApprovalPayload) approvalModel {
	return approvalModel{
		req:      req,
		options:  []string{"Approve", "Reject", "Auto-approve all"},
		selected: 0,
	}
}

func (m approvalModel) Init() tea.Cmd {
	return nil
}

func (m approvalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			} else {
				m.selected = len(m.options) - 1
			}
		case "down", "j":
			if m.selected < len(m.options)-1 {
				m.selected++
			} else {
				m.selected = 0
			}
		case "enter":
			m.chosen = true
			return m, tea.Quit
		case "a", "A":
			m.selected = 0
			m.chosen = true
			return m, tea.Quit
		case "r", "R":
			m.selected = 1
			m.chosen = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m approvalModel) View() string {
	s := strings.Builder{}

	for i, opt := range m.options {
		cursor := " " // no cursor
		if m.selected == i {
			cursor = "❯"
		}

		checked := "☐"
		if m.selected == i {
			checked = "☑"
		}

		// Style based on option
		var line string
		if m.selected == i {
			switch i {
			case 0:
				line = fmt.Sprintf("%s \033[1;32m%s %s\033[0m", cursor, checked, opt)
			case 1:
				line = fmt.Sprintf("%s \033[1;31m%s %s\033[0m", cursor, checked, opt)
			case 2:
				line = fmt.Sprintf("%s \033[1;34m%s %s\033[0m", cursor, checked, opt)
			default:
				line = fmt.Sprintf("%s %s %s", cursor, checked, opt)
			}
		} else {
			line = fmt.Sprintf("  \033[2m%s %s\033[0m", checked, opt)
		}

		s.WriteString(line + "\n")
	}

	return s.String()
}

func (c *CLIApprover) makeDecision(req api.ApprovalPayload, selected int) (api.Decision, bool, error) {
	switch selected {
	case 0:
		fmt.Println("\033[32m✓ Approved\033[0m")
		return api.Decision{
			Kind:       api.DecisionApprove,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	case 1:
		fmt.Println("\033[31m✗ Rejected\033[0m")
		return api.Decision{
			Kind:       api.DecisionReject,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	case 2:
		fmt.Println("\033[34m✓ Auto-approving all future actions\033[0m")
		return api.Decision{
			Kind:       api.DecisionApprove,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, true, nil
	}
	return api.Decision{
		Kind:       api.DecisionReject,
		RequestID:  req.RequestID,
		ToolCallID: req.ToolCallID,
	}, false, nil
}

// simpleApproval for non-interactive terminals
func (c *CLIApprover) simpleApproval(req api.ApprovalPayload) (api.Decision, bool, error) {
	fmt.Println("  (A)pprove  |  (R)eject  |  Auto-approve (all)")
	fmt.Print("\nChoice [A/r/all]: ")

	input, err := c.Reader.ReadString('\n')
	if err != nil {
		return api.Decision{
			Kind:       api.DecisionReject,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, err
	}

	input = strings.TrimSpace(strings.ToLower(input))

	switch input {
	case "", "a", "approve", "y", "yes":
		fmt.Println("\033[32m✓ Approved\033[0m")
		return api.Decision{
			Kind:       api.DecisionApprove,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	case "r", "reject", "n", "no":
		fmt.Println("\033[31m✗ Rejected\033[0m")
		return api.Decision{
			Kind:       api.DecisionReject,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	case "all", "auto":
		fmt.Println("\033[34m✓ Auto-approving all future actions\033[0m")
		return api.Decision{
			Kind:       api.DecisionApprove,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, true, nil
	default:
		fmt.Println("\033[33m? Defaulting to Approve\033[0m")
		return api.Decision{
			Kind:       api.DecisionApprove,
			RequestID:  req.RequestID,
			ToolCallID: req.ToolCallID,
		}, false, nil
	}
}
