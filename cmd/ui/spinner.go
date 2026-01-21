package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type spinnerModel struct {
	spinner  spinner.Model
	quitting bool
	msg      string
}

func initialSpinnerModel(msg string) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return spinnerModel{spinner: s, msg: msg}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "esc" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		// handle resize if needed
		return m, nil
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("\n%s %s", m.spinner.View(), m.msg)
}

// StartSpinner starts a spinner and returns a channel to stop it
// This is a simple blocking version or we can run it in a goroutine
// For this engine, we want it to run until we get a signal
func StartLoading(msg string) (chan struct{}, chan struct{}) {
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		m := initialSpinnerModel(msg)
		p := tea.NewProgram(m)

		// Run in a goroutine so we can kill it
		go func() {
			if _, err := p.Run(); err != nil {
				fmt.Println("Error running spinner:", err)
			}
			close(done)
		}()

		<-stop
		p.Quit()
	}()

	return stop, done
}

// StartInlineSpinner starts a lightweight inline spinner that updates on the same line
// Good for showing progress during tool execution without taking full screen
func StartInlineSpinner(toolName string) (chan struct{}, chan struct{}) {
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		idx := 0

		// Print initial line
		fmt.Printf("\n\n🔧 tool_call %s %s", toolName, frames[idx])

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				// Clear the spinner line and move to next line
				fmt.Print("\r\033[K") // Clear current line
				return
			case <-ticker.C:
				idx = (idx + 1) % len(frames)
				// Use carriage return to update spinner in place
				fmt.Printf("\r🔧 tool_call %s %s", toolName, frames[idx])
			}
		}
	}()

	return stop, done
}
