package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/prompts"
	"AgentEngine/pkg/logger"
)

// CompressConfig configures the compression behavior.
type CompressConfig struct {
	KeepTurns     int  // Number of recent turns to keep (default: 1)
	MaxMessages   int  // Max messages to keep after compression (default: 20)
	ForceCompress bool // Force compression even if below thresholds
}

// DefaultCompressConfig returns the default compression configuration.
func DefaultCompressConfig() CompressConfig {
	return CompressConfig{
		KeepTurns:   1,
		MaxMessages: 20,
	}
}

// CompressHistory compresses the session history by:
// 1. Using LLM to generate a summary of older messages
// 2. Keeping only the last N turns (or max M messages)
// 3. Storing the summary in session.Summary
func CompressHistory(ctx context.Context, llm LLM, session *api.Session, cfg CompressConfig) error {
	if cfg.KeepTurns <= 0 {
		cfg.KeepTurns = 1
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 20
	}

	totalMessages := len(session.Messages)
	turns := countTurns(session.Messages)

	// Decide if compression is needed
	needsCompression := cfg.ForceCompress ||
		totalMessages > cfg.MaxMessages ||
		turns > cfg.KeepTurns

	if !needsCompression {
		logger.Info("Compress", "No compression needed", map[string]interface{}{
			"total_messages": totalMessages,
			"turns":          turns,
			"max_messages":   cfg.MaxMessages,
			"keep_turns":     cfg.KeepTurns,
		})
		return nil
	}

	// Find the split point - keep last N turns
	splitIdx := findTurnSplitIndex(session.Messages, cfg.KeepTurns)

	// If split by turns doesn't help much, use message-based split
	if splitIdx == 0 || (totalMessages-splitIdx) > cfg.MaxMessages {
		// Find a safe split point that keeps at most MaxMessages
		splitIdx = findSafeMessageSplit(session.Messages, cfg.MaxMessages)
	}

	if splitIdx <= 0 {
		logger.Info("Compress", "No valid split point found", nil)
		return nil
	}

	oldMessages := session.Messages[:splitIdx]
	newMessages := session.Messages[splitIdx:]

	logger.Info("Compress", "Compressing history", map[string]interface{}{
		"old_messages": len(oldMessages),
		"new_messages": len(newMessages),
		"turns":        turns,
	})

	// Generate summary using LLM
	summary, err := generateSummary(ctx, llm, session.Summary, oldMessages)
	if err != nil {
		return fmt.Errorf("failed to generate summary: %w", err)
	}

	// Update session
	session.Summary = summary
	session.Messages = newMessages

	logger.Info("Compress", "Compression complete", map[string]interface{}{
		"summary_length":   len(summary),
		"messages_kept":    len(newMessages),
		"messages_removed": len(oldMessages),
	})

	return nil
}

// countTurns counts the number of user turns in the message history.
func countTurns(messages []api.LLMMessage) int {
	count := 0
	for _, m := range messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// findTurnSplitIndex finds the index to split messages, keeping the last N turns.
// A "turn" starts with a user message and includes all following assistant/tool messages.
// IMPORTANT: We must not split in the middle of a tool call sequence
// (assistant with tool_calls must be followed by all corresponding tool responses).
func findTurnSplitIndex(messages []api.LLMMessage, keepTurns int) int {
	// Find all valid split points (user message starts)
	// A valid split point is a user message that is NOT preceded by
	// an incomplete tool call sequence
	var validSplits []int

	// Track tool calls that need responses
	pendingToolCalls := make(map[string]bool)

	for i, m := range messages {
		// Track tool calls
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				pendingToolCalls[tc.ID] = true
			}
		}

		// Track tool responses
		if m.Role == "tool" && m.ToolCallID != "" {
			delete(pendingToolCalls, m.ToolCallID)
		}

		// A user message is a valid split point only if no pending tool calls
		if m.Role == "user" && len(pendingToolCalls) == 0 {
			validSplits = append(validSplits, i)
		}
	}

	// We need to keep the last N turns, so find the split point
	if len(validSplits) <= keepTurns {
		return 0 // Keep everything
	}

	// Return the split point that keeps exactly the last N turns
	splitIndex := len(validSplits) - keepTurns
	return validSplits[splitIndex]
}

// findSafeMessageSplit finds a split point that keeps at most maxMessages,
// ensuring we don't break tool call sequences and kept messages start correctly.
// The kept messages (after split) must NOT start with a 'tool' message,
// and must NOT start with an 'assistant' message that has tool_calls without responses.
func findSafeMessageSplit(messages []api.LLMMessage, maxMessages int) int {
	if len(messages) <= maxMessages {
		return 0
	}

	// We want to keep the last maxMessages, so split at len - maxMessages
	targetSplit := len(messages) - maxMessages

	// Find all valid split points
	// A valid split point is an index where:
	// 1. No pending tool calls from before this point
	// 2. The message AT this index is 'user' (safest to start a conversation)
	var validSplits []int
	pendingToolCalls := make(map[string]bool)

	for i, m := range messages {
		// Track tool calls
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				pendingToolCalls[tc.ID] = true
			}
		}

		// Track tool responses
		if m.Role == "tool" && m.ToolCallID != "" {
			delete(pendingToolCalls, m.ToolCallID)
		}

		// A user message with no pending tool calls is a valid split point
		if m.Role == "user" && len(pendingToolCalls) == 0 {
			validSplits = append(validSplits, i)
		}
	}

	// Find the best split point that's >= targetSplit
	for _, split := range validSplits {
		if split >= targetSplit {
			return split
		}
	}

	// If no valid point after target, use the last valid point before target
	for i := len(validSplits) - 1; i >= 0; i-- {
		if validSplits[i] > 0 {
			return validSplits[i]
		}
	}

	// No valid split found
	return 0
}

// generateSummary uses LLM to create a summary of the old messages.
func generateSummary(ctx context.Context, llm LLM, existingSummary string, messages []api.LLMMessage) (string, error) {
	var sb strings.Builder

	// Load prompt template from prompts package
	promptTemplate := prompts.DefaultLoader.Get(prompts.CompressSummary)
	if promptTemplate == "" {
		// Fallback if prompt not found
		promptTemplate = "Create a concise summary of this conversation for context continuation."
	}
	sb.WriteString(promptTemplate)
	sb.WriteString("\n\n")

	if existingSummary != "" {
		sb.WriteString("## Previous Context\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n## New Activity to Summarize\n")
	} else {
		sb.WriteString("## Conversation to Summarize\n")
	}

	// Build conversation summary - focus on outcomes
	for _, m := range messages {
		switch m.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("**User**: %s\n\n", truncateContent(m.Content, 300)))
		case "assistant":
			if m.Content != "" {
				sb.WriteString(fmt.Sprintf("**Assistant**: %s\n\n", truncateContent(m.Content, 300)))
			}
			if len(m.ToolCalls) > 0 {
				// Just list tool names, not details
				var tools []string
				for _, tc := range m.ToolCalls {
					tools = append(tools, tc.Name)
				}
				sb.WriteString(fmt.Sprintf("_[Used tools: %s]_\n", strings.Join(tools, ", ")))
			}
		case "tool":
			// For tool results, only include if it's an error or very short success
			if m.Content != "" && len(m.Content) < 100 {
				sb.WriteString(fmt.Sprintf("_Tool result: %s_\n", m.Content))
			}
		}
	}

	sb.WriteString("\n---\nProvide the summary now. Be concise but complete.")

	// Call LLM for summarization
	req := LLMRequest{
		Messages: []api.LLMMessage{
			{Role: "user", Content: sb.String()},
		},
		MaxTokens: 800,
	}

	stream, err := llm.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	// Collect response
	var result strings.Builder
	for {
		chunk, err := stream.Recv(ctx)
		if err != nil {
			break // EOF or error
		}
		if chunk.Delta != "" {
			result.WriteString(chunk.Delta)
		}
	}

	summary := strings.TrimSpace(result.String())
	if summary == "" {
		return existingSummary, nil // Keep existing if generation failed
	}

	return summary, nil
}

// truncateContent truncates content to maxLen characters.
func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// CompressResult contains the result of a compression operation.
type CompressResult struct {
	MessagesRemoved int    `json:"messages_removed"`
	MessagesKept    int    `json:"messages_kept"`
	SummaryLength   int    `json:"summary_length"`
	Summary         string `json:"summary"`
}

// ToJSON returns the result as a JSON string.
func (r CompressResult) ToJSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}
