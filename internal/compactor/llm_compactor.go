// Package compactor provides the generic LLM-based context compression
// implementation for SoloQueue's context window system.
//
// It defines a minimal ChatClient interface to avoid circular dependencies
// with the agent package. The upstream (cmd/soloqueue) provides an adapter
// that wraps agent.LLMClient into ChatClient.
package compactor

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Types ──────────────────────────────────────────────────────────────────

// ChatClient is the minimal LLM chat interface needed by Compactor.
//
// It avoids a direct dependency on agent.LLMClient to prevent circular imports.
// The upstream provides an adapter that wraps agent.LLMClient into ChatClient.
type ChatClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatRequest is the input for a chat completion call.
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
}

// ChatMessage is a single message in a chat request.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatResponse is the result of a chat completion call.
type ChatResponse struct {
	Content string
}

// ─── LLMCompactor ───────────────────────────────────────────────────────────

// compactSystemPrompt is the system prompt used for context compression.
// All built-in prompts must be in English.
const compactSystemPrompt = `You are a context compression assistant. Your task is to compress the following conversation history into a single concise summary.

Rules:
- Preserve all key decisions, conclusions, and outcomes
- Preserve tool call results that contain important data or state changes
- Preserve file paths, variable names, and other context clues needed for continuity
- Omit intermediate reasoning, failed attempts, and redundant tool outputs
- Keep the summary as compact as possible while retaining all essential information
- Output only the summary, no meta-commentary`

// CompactorOption 是 LLMCompactor 的可选配置
type CompactorOption func(*LLMCompactor)

// WithLogger 设置 compactor 的日志实例
func WithLogger(l *logger.Logger) CompactorOption {
	return func(c *LLMCompactor) { c.logger = l }
}

// LLMCompactor compresses conversation history using any LLM backend.
type LLMCompactor struct {
	client  ChatClient
	modelID string
	logger  *logger.Logger
}

// NewLLMCompactor creates a new LLMCompactor with the given client and model.
func NewLLMCompactor(client ChatClient, modelID string, opts ...CompactorOption) *LLMCompactor {
	c := &LLMCompactor{
		client:  client,
		modelID: modelID,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Compact compresses a slice of messages into a single summary string.
//
// It converts ctxwin.Message to ChatMessage, prepends a compression system
// prompt, and calls the LLM. Returns the summary content on success.
func (c *LLMCompactor) Compact(ctx context.Context, msgs []ctxwin.Message) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}

	if c.logger != nil {
		c.logger.InfoContext(ctx, logger.CatLLM, "compactor: starting",
			"msg_count", len(msgs), "model", c.modelID)
	}
	start := time.Now()

	// Build chat messages: compression system prompt + conversation history
	chatMsgs := make([]ChatMessage, 0, len(msgs)+1)
	chatMsgs = append(chatMsgs, ChatMessage{
		Role:    "system",
		Content: compactSystemPrompt,
	})

	for _, m := range msgs {
		content := m.Content
		if m.ReasoningContent != "" {
			content = fmt.Sprintf("%s\n\n[Reasoning]: %s", content, m.ReasoningContent)
		}
		chatMsgs = append(chatMsgs, ChatMessage{
			Role:    string(m.Role),
			Content: content,
		})
	}

	resp, err := c.client.Chat(ctx, ChatRequest{
		Model:    c.modelID,
		Messages: chatMsgs,
	})
	if err != nil {
		if c.logger != nil {
			c.logger.LogError(ctx, logger.CatLLM, "compactor: chat failed", err,
				"duration_ms", time.Since(start).Milliseconds())
		}
		return "", fmt.Errorf("compactor: chat failed: %w", err)
	}

	if c.logger != nil {
		c.logger.InfoContext(ctx, logger.CatLLM, "compactor: completed",
			"input_msgs", len(msgs),
			"output_len", len(resp.Content),
			"duration_ms", time.Since(start).Milliseconds())
	}

	return resp.Content, nil
}
