package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// FileSessionStore
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// sessionWrapper wraps Session with version for future migration.
type sessionWrapper struct {
	Version int          `json:"version"`
	Session *api.Session `json:"session"`
}

// FileSessionStore implements SessionStore using JSON files.
type FileSessionStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewFileSessionStore creates a new file-based session store.
func NewFileSessionStore(workspaceRoot string) (*FileSessionStore, error) {
	// workspaceRoot already points to workspace/ subdirectory
	baseDir := filepath.Join(workspaceRoot, "sessions")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}
	return &FileSessionStore{baseDir: baseDir}, nil
}

func (s *FileSessionStore) path(id string) string {
	return filepath.Join(s.baseDir, id+".json")
}

// validatePath ensures the path is within baseDir (workspace escape prevention).
func (s *FileSessionStore) validatePath(p string) error {
	absPath, err := filepath.Abs(p)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return ErrWorkspaceEscape
	}
	return nil
}

func (s *FileSessionStore) Get(ctx context.Context, id string) (*api.Session, error) {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}

	var wrapper sessionWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	// Guard against corrupt/empty session data
	if wrapper.Session == nil {
		return nil, fmt.Errorf("session data is nil for id: %s", id)
	}

	return wrapper.Session, nil
}

func (s *FileSessionStore) Put(ctx context.Context, id string, session *api.Session) error {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return err
	}

	wrapper := sessionWrapper{
		Version: 1,
		Session: session,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Atomic write: temp file + rename
	tmpPath := p + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath) // cleanup on failure
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func (s *FileSessionStore) Del(ctx context.Context, id string) error {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(p); os.IsNotExist(err) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

func (s *FileSessionStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			ids = append(ids, strings.TrimSuffix(name, ".json"))
		}
	}
	return ids, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// FilePlanStore
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// planWrapper wraps PlanPayload with version.
type planWrapper struct {
	Version int              `json:"version"`
	Plan    *api.PlanPayload `json:"plan"`
}

// FilePlanStore implements PlanStore using JSON files.
type FilePlanStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewFilePlanStore creates a new file-based plan store.
func NewFilePlanStore(workspaceRoot string) (*FilePlanStore, error) {
	// workspaceRoot already points to workspace/ subdirectory
	baseDir := filepath.Join(workspaceRoot, "plans")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plans directory: %w", err)
	}
	return &FilePlanStore{baseDir: baseDir}, nil
}

func (s *FilePlanStore) path(id string) string {
	return filepath.Join(s.baseDir, id+".json")
}

func (s *FilePlanStore) validatePath(p string) error {
	absPath, err := filepath.Abs(p)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	absBase, err := filepath.Abs(s.baseDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return ErrWorkspaceEscape
	}
	return nil
}

func (s *FilePlanStore) Get(ctx context.Context, id string) (*api.PlanPayload, error) {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read plan: %w", err)
	}

	var wrapper planWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to unmarshal plan: %w", err)
	}

	return wrapper.Plan, nil
}

func (s *FilePlanStore) Put(ctx context.Context, id string, plan *api.PlanPayload) error {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return err
	}

	wrapper := planWrapper{
		Version: 1,
		Plan:    plan,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Atomic write
	tmpPath := p + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func (s *FilePlanStore) Del(ctx context.Context, id string) error {
	p := s.path(id)
	if err := s.validatePath(p); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(p); os.IsNotExist(err) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to delete plan: %w", err)
	}
	return nil
}

func (s *FilePlanStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list plans: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			ids = append(ids, strings.TrimSuffix(name, ".json"))
		}
	}
	return ids, nil
}
