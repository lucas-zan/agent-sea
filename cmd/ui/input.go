// Package ui provides input/output utilities for the CLI
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputResult contains the result of user input
type InputResult struct {
	Value     string
	Submitted bool
	Cancelled bool
}

// Command represents a slash command
type Command struct {
	Name        string
	Description string
}

// DefaultCommands are the built-in slash commands
var DefaultCommands = []Command{
	{"/compress", "Compress conversation history, keep last 3 turns"},
	{"/init", "Initialize persona templates (project/local)"},
	{"/help", "Show help"},
	{"/quit", "Quit session"},
}

// inputModel is the bubbletea model for text input
type inputModel struct {
	textarea  textarea.Model
	submitted bool
	cancelled bool
	prompt    string

	history    []string
	historyPos int // -1 means "not browsing history"
	draft      string

	// Command completion
	commands     []Command
	showCommands bool
	selectedCmd  int
}

// NewInputModel creates a new input model with optional prompt
func newInputModel(prompt string, placeholder string) inputModel {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.Focus()

	// Configure textarea for chat input
	ta.CharLimit = 0 // No limit
	ta.SetWidth(80)
	ta.SetHeight(3) // Start with 3 lines, will auto-expand
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(true) // Alt+Enter for newline

	// Style
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	return inputModel{
		textarea:   ta,
		prompt:     prompt,
		historyPos: -1,
	}
}

func (m inputModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle command completion navigation
		if m.showCommands {
			switch msg.Type {
			case tea.KeyUp:
				if m.selectedCmd > 0 {
					m.selectedCmd--
				}
				return m, nil
			case tea.KeyDown:
				if m.selectedCmd < len(m.commands)-1 {
					m.selectedCmd++
				}
				return m, nil
			case tea.KeyTab:
				// Select current command
				if len(m.commands) > 0 {
					m.textarea.SetValue(m.commands[m.selectedCmd].Name + " ")
					m.showCommands = false
				}
				return m, nil
			case tea.KeyEnter:
				// Select and submit
				if len(m.commands) > 0 {
					m.textarea.SetValue(m.commands[m.selectedCmd].Name)
					m.submitted = true
					return m, tea.Quit
				}
			case tea.KeyEsc:
				m.showCommands = false
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			// Ctrl+C cancels input
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyCtrlD:
			// Ctrl+D also cancels/exits
			if m.textarea.Value() == "" {
				m.cancelled = true
				return m, tea.Quit
			}

		case tea.KeyEnter:
			// Enter submits (unless Alt is pressed for newline)
			if !msg.Alt {
				m.submitted = true
				return m, tea.Quit
			}

		case tea.KeyCtrlJ:
			// Ctrl+J inserts newline (like deepagents)
			m.textarea.InsertString("\n")
			return m, nil

		case tea.KeyCtrlP:
			// Ctrl+P: previous input (history)
			m.prevHistory()
			return m, nil

		case tea.KeyCtrlN:
			// Ctrl+N: next input (history)
			m.nextHistory()
			return m, nil
		}

	case tea.WindowSizeMsg:
		// Adjust width based on terminal size
		m.textarea.SetWidth(msg.Width - 10)
	}

	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	// Check if should show command completion
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
		m.showCommands = true
		m.commands = filterCommands(DefaultCommands, val)
		if m.selectedCmd >= len(m.commands) {
			m.selectedCmd = 0
		}
	} else {
		m.showCommands = false
	}

	return m, tea.Batch(cmds...)
}

// filterCommands returns commands matching the prefix
func filterCommands(cmds []Command, prefix string) []Command {
	if prefix == "/" {
		return cmds
	}
	var filtered []Command
	for _, c := range cmds {
		if strings.HasPrefix(c.Name, prefix) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func (m inputModel) View() string {
	var b strings.Builder

	// Prompt
	if m.prompt != "" {
		b.WriteString(m.prompt)
	}

	// Textarea
	b.WriteString(m.textarea.View())

	// Command completion menu
	if m.showCommands && len(m.commands) > 0 {
		b.WriteString("\n")
		menuStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

		var menuContent strings.Builder
		for i, cmd := range m.commands {
			if i == m.selectedCmd {
				// Highlighted
				menuContent.WriteString(lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("86")).
					Render(fmt.Sprintf("> %s", cmd.Name)))
			} else {
				menuContent.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("245")).
					Render(fmt.Sprintf("  %s", cmd.Name)))
			}
			menuContent.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render(fmt.Sprintf("  %s", cmd.Description)))
			if i < len(m.commands)-1 {
				menuContent.WriteString("\n")
			}
		}
		b.WriteString(menuStyle.Render(menuContent.String()))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("↑↓ Select | Tab Complete | Enter Run | Esc Close"))
	} else {
		// Help text
		helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Enter Submit | Ctrl+J/Alt+Enter Newline | Ctrl+P/Ctrl+N History | Ctrl+C Cancel"))
	}

	return b.String()
}

func (m *inputModel) prevHistory() {
	if len(m.history) == 0 {
		return
	}
	if m.historyPos == -1 {
		m.draft = m.textarea.Value()
		m.historyPos = len(m.history) - 1
	} else if m.historyPos > 0 {
		m.historyPos--
	}
	m.textarea.SetValue(m.history[m.historyPos])
}

func (m *inputModel) nextHistory() {
	if len(m.history) == 0 || m.historyPos == -1 {
		return
	}
	if m.historyPos < len(m.history)-1 {
		m.historyPos++
		m.textarea.SetValue(m.history[m.historyPos])
		return
	}
	m.historyPos = -1
	m.textarea.SetValue(m.draft)
}

// ReadInput reads multiline input from the user using bubbles textarea
// Returns the input value, whether it was submitted, and any error
func ReadInput(prompt string) (InputResult, error) {
	m := newInputModel(prompt, "Type a message...")
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return InputResult{}, fmt.Errorf("input error: %w", err)
	}

	result := finalModel.(inputModel)
	return InputResult{
		Value:     strings.TrimSpace(result.textarea.Value()),
		Submitted: result.submitted,
		Cancelled: result.cancelled,
	}, nil
}

// ReadInputWithHistory reads input with previously entered values available
func ReadInputWithHistory(prompt string, history []string) (InputResult, error) {
	m := newInputModel(prompt, "Type a message...")
	m.history = append([]string(nil), history...)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return InputResult{}, fmt.Errorf("input error: %w", err)
	}

	result := finalModel.(inputModel)
	return InputResult{
		Value:     strings.TrimSpace(result.textarea.Value()),
		Submitted: result.submitted,
		Cancelled: result.cancelled,
	}, nil
}

// Confirm asks for yes/no confirmation
func Confirm(prompt string) (bool, error) {
	fmt.Printf("%s [y/N]: ", prompt)
	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false, nil // Default to no
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes", nil
}
