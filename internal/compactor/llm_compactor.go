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

// charLevelTruncate aggressively truncates a string, keeping headRatio
// from the start and tailRatio from the end.  Used by the compactor to
// bound its own input so it never triggers an API 400.
func charLevelTruncate(s string, headRatio, tailRatio float64) string {
	runes := []rune(s)
	n := len(runes)
	headLen := int(float64(n) * headRatio)
	tailLen := int(float64(n) * tailRatio)
	if headLen+tailLen >= n || (headLen == 0 && tailLen == 0) {
		return s
	}
	head := string(runes[:headLen])
	tail := string(runes[n-tailLen:])
	omitted := n - headLen - tailLen
	return head + fmt.Sprintf("\n[...omitted %d characters...]\n", omitted) + tail
}

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
	ProviderID string
	Model      string
	Messages   []ChatMessage
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
- Output only the summary, no meta-commentary

At the end of your output, if the conversation contains important facts, decisions,
user preferences, or solutions worth remembering, include a <memories> section:

<memories>
- User prefers HikariCP for connection pooling
- Connection pool exhaustion: caused by unclosed connections in UserService
- Project uses Spring Boot 3.2 with Java 21
</memories>

Each memory should be a concise, standalone statement. Only include genuinely
important information, not casual talk. If nothing is worth saving, omit the
entire <memories> section.`

// CompactorOption is an optional configuration for LLMCompactor.
type CompactorOption func(*LLMCompactor)

// WithLogger sets the logger instance for the compactor.
func WithLogger(l *logger.Logger) CompactorOption {
	return func(c *LLMCompactor) { c.logger = l }
}

// LLMCompactor compresses conversation history using any LLM backend.
type LLMCompactor struct {
	client     ChatClient
	providerID string
	modelID    string
	logger     *logger.Logger
}

// NewLLMCompactor creates a new LLMCompactor with the given client, provider, and model.
func NewLLMCompactor(client ChatClient, providerID, modelID string, opts ...CompactorOption) *LLMCompactor {
	c := &LLMCompactor{
		client:     client,
		providerID: providerID,
		modelID:    modelID,
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
		ProviderID: c.providerID,
		Model:      c.modelID,
		Messages:   chatMsgs,
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
