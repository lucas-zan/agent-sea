package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// MockLLM is a deterministic local LLM implementation for development/testing.
// It never calls tools.
type MockLLM struct{}

func (m *MockLLM) Stream(ctx context.Context, req LLMRequest) (LLMStream, error) {
	var lastUser string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUser = req.Messages[i].Content
			break
		}
	}

	var b strings.Builder
	b.WriteString("[Mock LLM]\n")
	b.WriteString(fmt.Sprintf("messages=%d tools=%d\n", len(req.Messages), len(req.Tools)))
	if lastUser != "" {
		b.WriteString("last_user=")
		b.WriteString(truncateMock(lastUser, 200))
		b.WriteString("\n")
	}
	b.WriteString("Set LLM_API_KEY to use a real OpenAI-compatible model.\n")

	return &mockStream{content: b.String()}, nil
}

type mockStream struct {
	content string
	once    sync.Once
	chunks  []LLMChunk
	closed  bool
}

func (s *mockStream) Recv(ctx context.Context) (LLMChunk, error) {
	if s.closed {
		return LLMChunk{}, io.EOF
	}

	s.once.Do(func() {
		// Chunk the content so UI sees streaming behavior.
		const step = 32
		for i := 0; i < len(s.content); i += step {
			end := i + step
			if end > len(s.content) {
				end = len(s.content)
			}
			s.chunks = append(s.chunks, LLMChunk{Delta: s.content[i:end]})
		}
		s.chunks = append(s.chunks, LLMChunk{FinishReason: "stop"})
	})

	if len(s.chunks) == 0 {
		s.closed = true
		return LLMChunk{}, io.EOF
	}

	ch := s.chunks[0]
	s.chunks = s.chunks[1:]
	if len(s.chunks) == 0 {
		// Next Recv will return io.EOF after FinishReason is observed by the caller.
		s.closed = true
	}
	return ch, nil
}

func (s *mockStream) Close() error {
	s.closed = true
	return nil
}

func truncateMock(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
