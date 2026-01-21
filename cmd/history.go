package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HistoryEntry represents a single input history item
type HistoryEntry struct {
	Timestamp time.Time `json:"ts"`
	Input     string    `json:"input"`
}

// HistoryManager manages the persistence of user input history
type HistoryManager struct {
	path string
	mu   sync.Mutex
}

// NewHistoryManager creates a new history manager pointing to workspace/history/input.jsonl
func NewHistoryManager(workspaceRoot string) (*HistoryManager, error) {
	dir := filepath.Join(workspaceRoot, "history")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}

	return &HistoryManager{
		path: filepath.Join(dir, "input.jsonl"),
	}, nil
}

// Load reads all history entries and returns valid inputs as a string slice
func (h *HistoryManager) Load() ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No history yet
		}
		return nil, err
	}

	var inputs []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}
		if entry.Input != "" {
			inputs = append(inputs, entry.Input)
		}
	}
	return inputs, nil
}

// Append adds a new input to the history file
func (h *HistoryManager) Append(input string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := HistoryEntry{
		Timestamp: time.Now(),
		Input:     input,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}
