package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// JSONLEventLog
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// JSONLEventLog implements EventLog using JSONL files (one per session).
type JSONLEventLog struct {
	baseDir string
	mu      sync.Mutex
}

// NewJSONLEventLog creates a new JSONL-based event log.
func NewJSONLEventLog(workspaceRoot string) (*JSONLEventLog, error) {
	// workspaceRoot already points to workspace/ subdirectory
	baseDir := filepath.Join(workspaceRoot, "events")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create events directory: %w", err)
	}
	return &JSONLEventLog{baseDir: baseDir}, nil
}

func (l *JSONLEventLog) path(sessionID string) string {
	return filepath.Join(l.baseDir, sessionID+".jsonl")
}

func (l *JSONLEventLog) validatePath(p string) error {
	absPath, err := filepath.Abs(p)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	absBase, err := filepath.Abs(l.baseDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return ErrWorkspaceEscape
	}
	return nil
}

// Append adds an event to the log.
func (l *JSONLEventLog) Append(ctx context.Context, e api.Event) error {
	if e.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}

	p := l.path(e.SessionID)
	if err := l.validatePath(p); err != nil {
		return err
	}

	// Set timestamp if not provided
	if e.Ts.IsZero() {
		e.Ts = time.Now()
	}

	// Set default version
	if e.Version == 0 {
		e.Version = 1
	}

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("failed to append event: %w", err)
	}
	return nil
}

// Stream returns an event stream for a session.
func (l *JSONLEventLog) Stream(ctx context.Context, sessionID string) (api.EventStream, error) {
	p := l.path(sessionID)
	if err := l.validatePath(p); err != nil {
		return nil, err
	}

	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return &emptyEventStream{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open events file: %w", err)
	}

	return &fileEventStream{
		file:    f,
		scanner: bufio.NewScanner(f),
	}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Event Stream Implementations
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// fileEventStream reads events from a JSONL file.
type fileEventStream struct {
	file    *os.File
	scanner *bufio.Scanner
}

func (s *fileEventStream) Recv(ctx context.Context) (api.Event, error) {
	select {
	case <-ctx.Done():
		return api.Event{}, ctx.Err()
	default:
	}

	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return api.Event{}, fmt.Errorf("failed to scan event: %w", err)
		}
		return api.Event{}, io.EOF
	}

	var e api.Event
	if err := json.Unmarshal(s.scanner.Bytes(), &e); err != nil {
		return api.Event{}, fmt.Errorf("failed to unmarshal event: %w", err)
	}
	return e, nil
}

func (s *fileEventStream) Close() error {
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// emptyEventStream returns EOF immediately.
type emptyEventStream struct{}

func (s *emptyEventStream) Recv(ctx context.Context) (api.Event, error) {
	return api.Event{}, io.EOF
}

func (s *emptyEventStream) Close() error {
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Channel Event Stream (for runtime use)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ChannelEventStream implements EventStream using a channel.
type ChannelEventStream struct {
	ch     chan api.Event
	closed bool
	mu     sync.Mutex
}

// NewChannelEventStream creates a new channel-based event stream.
func NewChannelEventStream(bufferSize int) *ChannelEventStream {
	return &ChannelEventStream{
		ch: make(chan api.Event, bufferSize),
	}
}

// Send sends an event to the stream.
func (s *ChannelEventStream) Send(e api.Event) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("stream is closed")
	}
	s.mu.Unlock()

	s.ch <- e
	return nil
}

// Recv receives an event from the stream.
func (s *ChannelEventStream) Recv(ctx context.Context) (api.Event, error) {
	select {
	case <-ctx.Done():
		return api.Event{}, ctx.Err()
	case e, ok := <-s.ch:
		if !ok {
			return api.Event{}, io.EOF
		}
		return e, nil
	}
}

// Close closes the stream.
func (s *ChannelEventStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	return nil
}
