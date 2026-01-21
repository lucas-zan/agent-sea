// Package prompts provides utilities for loading prompt templates.
package prompts

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed *.md
var embeddedPrompts embed.FS

// Loader loads prompt templates from files.
type Loader struct {
	projectRoot string
	cache       map[string]string
	mu          sync.RWMutex
}

// NewLoader creates a new prompt loader.
// If projectRoot is provided, it will look for prompts in <projectRoot>/prompts/ first.
func NewLoader(projectRoot string) *Loader {
	return &Loader{
		projectRoot: projectRoot,
		cache:       make(map[string]string),
	}
}

// Get returns the content of a prompt template by name.
// It first checks for a custom prompt in <projectRoot>/prompts/<name>.md,
// then falls back to the embedded default.
func (l *Loader) Get(name string) string {
	l.mu.RLock()
	if cached, ok := l.cache[name]; ok {
		l.mu.RUnlock()
		return cached
	}
	l.mu.RUnlock()

	content := l.load(name)

	l.mu.Lock()
	l.cache[name] = content
	l.mu.Unlock()

	return content
}

func (l *Loader) load(name string) string {
	filename := name + ".md"

	// Try project-level custom prompt first
	if l.projectRoot != "" {
		customPath := filepath.Join(l.projectRoot, "prompts", filename)
		if content, err := os.ReadFile(customPath); err == nil {
			return strings.TrimSpace(string(content))
		}
	}

	// Fall back to embedded default
	if content, err := embeddedPrompts.ReadFile(filename); err == nil {
		return strings.TrimSpace(string(content))
	}

	return ""
}

// ClearCache clears the prompt cache to force reloading.
func (l *Loader) ClearCache() {
	l.mu.Lock()
	l.cache = make(map[string]string)
	l.mu.Unlock()
}

// Prompt names
const (
	CompressSummary   = "compress_summary"
	CompressInjection = "compress_injection"
)

// DefaultLoader is a loader with no project root (uses embedded prompts only).
var DefaultLoader = NewLoader("")
