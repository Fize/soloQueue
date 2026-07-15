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
	"regexp"
	"strings"
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
//
// Reasoning content is intentionally dropped from the compactor's input: the
// compactor only needs the user-visible surface of the conversation. The
// previous behaviour of inlining reasoning as "[Reasoning]: ..." caused the
// LLM to echo the same marker into the summary, leaking internal chain-of-
// thought into the timeline and the chat UI.
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
		chatMsgs = append(chatMsgs, ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
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

	// Defensive: the LLM may still emit "[Reasoning]: ..." blocks even though
	// the input no longer contains them. Strip them before persisting so the
	// chat UI never shows internal chain-of-thought to the user.
	cleaned := stripReasoningBlocks(resp.Content)

	if c.logger != nil {
		c.logger.InfoContext(ctx, logger.CatLLM, "compactor: completed",
			"input_msgs", len(msgs),
			"output_len", len(cleaned),
			"duration_ms", time.Since(start).Milliseconds())
	}

	return cleaned, nil
}

// reasoningBlockPattern matches a "[Reasoning]: ..." block (possibly multi-line)
// that an LLM may emit in its response. Non-greedy and case-insensitive.
var reasoningBlockPattern = regexp.MustCompile(`(?is)\[reasoning\]:\s*.*?(?:\n\s*\n|\z)`)

// stripReasoningBlocks removes any "[Reasoning]: ..." block from the LLM
// response. It is a no-op if no such block is present.
func stripReasoningBlocks(s string) string {
	return strings.TrimSpace(reasoningBlockPattern.ReplaceAllString(s, ""))
}
