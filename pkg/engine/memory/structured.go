package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
)

// StructuredManager stores structured memory entries under <workspaceRoot>/workspace/memory/.
// It is intentionally simple: a small JSON file per source with atomic writes.
type StructuredManager struct {
	workspaceRoot string
	mu            sync.Mutex
}

func NewStructuredManager(workspaceRoot string) *StructuredManager {
	return &StructuredManager{workspaceRoot: workspaceRoot}
}

func (m *StructuredManager) List(ctx context.Context, source api.MemorySource) ([]api.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.load(source)
	if err != nil {
		return nil, err
	}
	out := make([]api.MemoryEntry, len(entries))
	copy(out, entries)
	return out, nil
}

func (m *StructuredManager) Search(ctx context.Context, query string) ([]api.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, nil
	}

	user, err := m.load(api.MemorySourceUser)
	if err != nil {
		return nil, err
	}
	project, err := m.load(api.MemorySourceProject)
	if err != nil {
		return nil, err
	}

	var matches []api.MemoryEntry
	for _, e := range append(project, user...) {
		if matchMemory(query, e) {
			matches = append(matches, e)
		}
	}
	return matches, nil
}

func (m *StructuredManager) Add(ctx context.Context, entry api.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Source != api.MemorySourceUser && entry.Source != api.MemorySourceProject {
		return fmt.Errorf("invalid memory source: %q", entry.Source)
	}
	if strings.TrimSpace(entry.Content) == "" {
		return fmt.Errorf("memory content is required")
	}

	entries, err := m.load(entry.Source)
	if err != nil {
		return err
	}

	now := time.Now()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("mem_%d", now.UnixNano())
	}
	for _, e := range entries {
		if e.ID == entry.ID {
			return fmt.Errorf("memory entry already exists: %s", entry.ID)
		}
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	entries = append(entries, entry)
	return m.save(entry.Source, entries)
}

func (m *StructuredManager) Update(ctx context.Context, entry api.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.Source != api.MemorySourceUser && entry.Source != api.MemorySourceProject {
		return fmt.Errorf("invalid memory source: %q", entry.Source)
	}
	if entry.ID == "" {
		return fmt.Errorf("memory id is required")
	}

	entries, err := m.load(entry.Source)
	if err != nil {
		return err
	}

	found := false
	now := time.Now()
	for i := range entries {
		if entries[i].ID != entry.ID {
			continue
		}
		found = true
		created := entries[i].CreatedAt
		entries[i] = entry
		if entries[i].CreatedAt.IsZero() {
			entries[i].CreatedAt = created
		}
		entries[i].UpdatedAt = now
		break
	}
	if !found {
		return fmt.Errorf("memory entry not found: %s", entry.ID)
	}

	return m.save(entry.Source, entries)
}

func (m *StructuredManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory id is required")
	}

	// Try both sources.
	if deleted, err := m.deleteFrom(api.MemorySourceProject, id); err != nil {
		return err
	} else if deleted {
		return nil
	}
	if deleted, err := m.deleteFrom(api.MemorySourceUser, id); err != nil {
		return err
	} else if deleted {
		return nil
	}

	return fmt.Errorf("memory entry not found: %s", id)
}

func (m *StructuredManager) deleteFrom(source api.MemorySource, id string) (bool, error) {
	entries, err := m.load(source)
	if err != nil {
		return false, err
	}
	out := entries[:0]
	deleted := false
	for _, e := range entries {
		if e.ID == id {
			deleted = true
			continue
		}
		out = append(out, e)
	}
	if !deleted {
		return false, nil
	}
	return true, m.save(source, out)
}

func matchMemory(query string, e api.MemoryEntry) bool {
	if strings.Contains(strings.ToLower(e.ID), query) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Content), query) {
		return true
	}
	for _, t := range e.Tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

func (m *StructuredManager) load(source api.MemorySource) ([]api.MemoryEntry, error) {
	path, err := m.pathFor(source)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []api.MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse memory file %s: %w", path, err)
	}
	return entries, nil
}

func (m *StructuredManager) save(source api.MemorySource, entries []api.MemoryEntry) error {
	path, err := m.pathFor(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *StructuredManager) pathFor(source api.MemorySource) (string, error) {
	switch source {
	case api.MemorySourceUser:
		return filepath.Join(m.workspaceRoot, "memory", "user.json"), nil
	case api.MemorySourceProject:
		return filepath.Join(m.workspaceRoot, "memory", "project.json"), nil
	default:
		return "", fmt.Errorf("invalid memory source: %q", source)
	}
}
