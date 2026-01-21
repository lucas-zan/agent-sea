package tools

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages a collection of tools
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new empty tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
// Returns an error if a tool with the same name already exists
func (r *Registry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}

	r.tools[name] = tool
	return nil
}

// MustRegister adds a tool to the registry, panicking on error
func (r *Registry) MustRegister(tool Tool) {
	if err := r.Register(tool); err != nil {
		panic(err)
	}
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	return tool, ok
}

// All returns all registered tools
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, tool)
	}

	// Sort by name for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})

	return result
}

// Names returns all registered tool names
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered tools
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// DefaultRegistry creates a registry with all built-in tools
func DefaultRegistry(workspaceRoot string) *Registry {
	r := NewRegistry()

	// File tools
	r.MustRegister(NewLsTool(workspaceRoot))
	r.MustRegister(NewReadFileTool(workspaceRoot))
	r.MustRegister(NewWriteFileTool(workspaceRoot))
	r.MustRegister(NewEditFileTool(workspaceRoot))

	// Search tools
	r.MustRegister(NewGlobTool(workspaceRoot))
	r.MustRegister(NewGrepTool(workspaceRoot))

	// Diagnostics tools
	r.MustRegister(NewLSPDiagnosticsTool(workspaceRoot))

	// Shell tool
	r.MustRegister(NewShellTool(workspaceRoot))

	return r
}
