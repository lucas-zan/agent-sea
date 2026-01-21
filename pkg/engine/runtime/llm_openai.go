package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/logger"
)

// OpenAILLM implements the runtime LLM interface using an OpenAI-compatible chat/completions endpoint.
type OpenAILLM struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAILLMFromEnv builds an OpenAI-compatible client from environment variables.
// - LLM_BASE_URL (default: https://api.openai.com/v1)
// - LLM_API_KEY (required; if missing, caller should use MockLLM)
// - LLM_MODEL (default: gpt-4o-mini)
func NewOpenAILLMFromEnv() (*OpenAILLM, error) {
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY environment variable is required")
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	return NewOpenAILLM(baseURL, apiKey, model), nil
}

func NewOpenAILLM(baseURL, apiKey, model string) *OpenAILLM {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &OpenAILLM{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 24 * time.Hour, // Long timeout for streaming long content
		},
	}
}

func (c *OpenAILLM) Stream(ctx context.Context, req LLMRequest) (LLMStream, error) {
	payload := openAIChatCompletionRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(req.Messages),
		Stream:      true,
		Temperature: 0.1,
	}
	if req.MaxTokens > 0 {
		payload.MaxTokens = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		payload.Tools = toOpenAITools(req.Tools)
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("LLM", "Failed to marshal request", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, err
	}

	// Log request details (without full message content for brevity)
	logger.Info("LLM", "Sending request to LLM API", map[string]interface{}{
		"url":           c.baseURL + "/chat/completions",
		"model":         c.model,
		"message_count": len(payload.Messages),
		"tool_count":    len(payload.Tools),
		"max_tokens":    payload.MaxTokens,
	})

	url := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		logger.Error("LLM", "Failed to create HTTP request", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logger.Error("LLM", "HTTP request failed", map[string]interface{}{
			"error": err.Error(),
			"url":   url,
		})
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		errMsg := strings.TrimSpace(string(raw))

		// Log the full error response for debugging
		logger.Error("LLM", "LLM API returned error", map[string]interface{}{
			"status_code": resp.StatusCode,
			"error":       errMsg,
			"url":         url,
			"model":       c.model,
		})

		// Log the FULL request payload for debugging when there's an error
		// This is crucial for debugging issues like null content fields
		var debugPayload map[string]interface{}
		if err := json.Unmarshal(body, &debugPayload); err == nil {
			logger.Warn("LLM", "Full request payload that caused error", map[string]interface{}{
				"payload": debugPayload,
			})
		} else {
			// If we can't parse as JSON, log the raw body
			logger.Warn("LLM", "Raw request body that caused error", map[string]interface{}{
				"body": string(body),
			})
		}

		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, errMsg)
	}

	logger.Info("LLM", "LLM API request successful, starting stream", map[string]interface{}{
		"status_code": resp.StatusCode,
	})

	return newOpenAIStream(resp.Body), nil
}

type openAIChatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []openAIChatMsg   `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	Tools       []openAITool      `json:"tools,omitempty"`
	ToolChoice  string            `json:"tool_choice,omitempty"`
	StreamOpts  map[string]any    `json:"stream_options,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	User        string            `json:"user,omitempty"`
}

type openAITool struct {
	Type     string     `json:"type"`
	Function openAIFunc `json:"function"`
}

type openAIFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"` // removed omitempty to avoid null/undefined

	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	Index    int            `json:"index"`
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFuncCall `json:"function"`
}

type openAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string           `json:"content,omitempty"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	// Error response from API (e.g., stream read error)
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func toOpenAITools(tools []api.ToolSchema) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func toOpenAIMessages(messages []api.LLMMessage) []openAIChatMsg {
	out := make([]openAIChatMsg, 0, len(messages))
	for _, msg := range messages {
		// Ensure content is never null - use empty string if no content
		content := msg.Content
		if content == "" && msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// For assistant messages with tool calls, content can be empty string
			content = ""
		}

		m := openAIChatMsg{
			Role:    msg.Role,
			Content: content,
		}
		if msg.Role == "tool" {
			m.ToolCallID = msg.ToolCallID
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIFuncCall{
						Name:      tc.Name,
						Arguments: tc.Args,
					},
				})
			}
		}
		out = append(out, m)
	}
	return out
}

type openAIStream struct {
	body   io.ReadCloser
	reader *bufio.Reader

	mu    sync.Mutex
	queue []LLMChunk
	done  bool

	toolBuilders map[int]*openAIToolCallBuilder
}

type openAIToolCallBuilder struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

func newOpenAIStream(body io.ReadCloser) *openAIStream {
	return &openAIStream{
		body:         body,
		reader:       bufio.NewReader(body),
		toolBuilders: make(map[int]*openAIToolCallBuilder),
	}
}

func (s *openAIStream) Recv(ctx context.Context) (LLMChunk, error) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		ch := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return ch, nil
	}
	if s.done {
		s.mu.Unlock()
		return LLMChunk{}, io.EOF
	}
	s.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return LLMChunk{}, ctx.Err()
		default:
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				s.mu.Lock()
				s.done = true
				s.mu.Unlock()
				return LLMChunk{}, io.EOF
			}
			return LLMChunk{}, err
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			s.mu.Lock()
			s.done = true
			s.mu.Unlock()
			return LLMChunk{}, io.EOF
		}

		var chunk openAIStreamChunk
		logger.Info("LLM", "Received chunk", map[string]interface{}{
			"service": "agent-engine",
			"chunk":   data,
		})
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.Error("LLM", "Failed to unmarshal chunk", map[string]interface{}{
				"service": "agent-engine",
			})
			continue
		}

		// Handle API error response
		if chunk.Error != nil {
			logger.Error("LLM", "API returned error in stream", map[string]interface{}{
				"error_type":    chunk.Error.Type,
				"error_message": chunk.Error.Message,
			})
			s.mu.Lock()
			s.done = true
			s.mu.Unlock()
			return LLMChunk{}, fmt.Errorf("LLM stream error: %s", chunk.Error.Message)
		}

		if len(chunk.Choices) == 0 {
			logger.Info("LLM", "Empty chunk received", map[string]interface{}{
				"service": "agent-engine",
			})
			continue
		}

		delta := chunk.Choices[0].Delta
		finish := chunk.Choices[0].FinishReason

		// Tool call deltas are buffered across chunks until finish_reason == "tool_calls".
		// Also emit ToolArgDelta for streaming UI display.
		if len(delta.ToolCalls) > 0 {
			var argDelta string
			s.mu.Lock()
			for _, tc := range delta.ToolCalls {
				b := s.toolBuilders[tc.Index]
				if b == nil {
					b = &openAIToolCallBuilder{index: tc.Index}
					s.toolBuilders[tc.Index] = b
				}
				if tc.ID != "" {
					b.id = tc.ID
				}
				if tc.Function.Name != "" {
					b.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					b.args.WriteString(tc.Function.Arguments)
					argDelta += tc.Function.Arguments // Collect for streaming display
				}
			}
			s.mu.Unlock()

			// Return tool arg delta for UI streaming display (gray text)
			if argDelta != "" {
				return LLMChunk{ToolArgDelta: argDelta}, nil
			}
		}

		// Text delta.
		if delta.Content != "" {
			return LLMChunk{Delta: delta.Content}, nil
		}

		if finish != "" {
			logger.Info("LLM", "Stream finish reason received", map[string]interface{}{
				"finish_reason": finish,
				"tool_count":    len(s.toolBuilders),
			})

			s.mu.Lock()
			if s.queue == nil {
				s.queue = make([]LLMChunk, 0, 8)
			}

			if finish == "tool_calls" {
				// Sort keys to maintain order? Not strictly needed if returning LLMToolCalls individually
				// but map iteration order is random. Better to iterate 0..n
				// Find max index
				maxIdx := -1
				for i := range s.toolBuilders {
					if i > maxIdx {
						maxIdx = i
					}
				}

				for i := 0; i <= maxIdx; i++ {
					b := s.toolBuilders[i]
					if b == nil || b.name == "" {
						continue
					}
					s.queue = append(s.queue, LLMChunk{
						ToolCall: &api.LLMToolCall{ID: b.id, Name: b.name, Args: b.args.String()},
					})
				}
				// Reset builders for the next model call.
				s.toolBuilders = make(map[int]*openAIToolCallBuilder)
			}

			s.queue = append(s.queue, LLMChunk{FinishReason: finish})
			ch := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			return ch, nil
		}
	}
}
func (s *openAIStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	s.done = true
	return s.body.Close()
}
