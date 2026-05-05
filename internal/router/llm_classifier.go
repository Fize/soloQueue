package router

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── LLM Classifier ─────────────────────────────────────────────────────────
//
// LLMClassifier uses a lightweight, non-thinking LLM call for semantic
// classification when fast-track rules produce insufficient confidence.
//
// Design principles:
//   - Uses the "fast" model with ThinkingEnabled=false (Non-CoT) for speed
//   - Hard timeout of 4 seconds; timeout/error gracefully degrades to L1
//   - sync.Map cache avoids repeated classification of similar prompts
//   - Shares the same LLMClient as agents (concurrent-safe by contract)

const (
	// llmClassifierTimeout is the hard deadline for LLM classification calls.
	// Beyond this, we fall back to the fast-track result.
	llmClassifierTimeout = 4 * time.Second

	// llmClassifierMaxTokens caps the output to keep responses compact.
	llmClassifierMaxTokens = 128
)

// llmClassifierSystemPrompt is the system prompt for the task classifier.
// It forces structured JSON output for fast, deterministic parsing.
const llmClassifierSystemPrompt = `You are a task complexity classifier. Analyze the user's request and respond with ONLY a valid JSON object. No other text, no markdown, no explanation.

Classification rules:
- "intent": "chat" (explanation, question, discussion, greeting) or "action" (requires executing commands, modifying files, or producing artifacts)
- "level": 0 (pure conversation, no action needed), 1 (single clear action with clear target), 2 (multiple steps, cross-concern coordination, needs planning), 3 (architecture changes, deep investigation, high uncertainty, system-wide impact)
- "reason": one short English sentence explaining your decision

IMPORTANT OVERRIDE RULES:
- If user explicitly requests deep thinking or careful analysis (e.g., "仔细想", "think carefully", "thorough"), raise level by 1
- Greetings and chitchat are always level 0
- Simple single-target actions (fix a bug, add a field, rename something) are level 1
- Multi-target changes, migrations, integrations are level 2
- Rewrites, architectural decisions, complex debugging, system design are level 3

Output format (ONLY this JSON, nothing else):
{"intent":"chat|action","level":0,"reason":"..."}`

// LLMClassifier performs semantic task classification using a lightweight LLM call.
type LLMClassifier struct {
	client  agent.LLMClient
	model   string // API model name for classification (e.g., "deepseek-v4-flash")
	timeout time.Duration
	cache   sync.Map // uint64 → ClassificationResult
	logger  *logger.Logger
}

// NewLLMClassifier creates a new LLM-based classifier.
//
// Parameters:
//   - client: shared LLM client (concurrent-safe)
//   - model: API model name to use (typically the "fast" model without thinking)
//   - logger: optional logger (nil = creates minimal system-layer logger)
func NewLLMClassifier(client agent.LLMClient, model string, l *logger.Logger) *LLMClassifier {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}
	return &LLMClassifier{
		client:  client,
		model:   model,
		timeout: llmClassifierTimeout,
		logger:  l,
	}
}

// Classify performs LLM-based semantic classification of a user prompt.
//
// Flow:
//  1. Check cache (FNV-64 hash of normalized prompt)
//  2. Apply timeout (4s hard limit)
//  3. Call LLM.Chat() synchronously (no thinking, no tools, JSON output)
//  4. Parse structured JSON response
//  5. On timeout/error → return safe L1 default (never blocks the pipeline)
//  6. On success → cache and return
func (lc *LLMClassifier) Classify(ctx context.Context, prompt string, _ ClassificationLevel) (ClassificationResult, error) {
	// 1. Check cache
	key := hashPrompt(prompt)
	if cached, ok := lc.cache.Load(key); ok {
		result := cached.(ClassificationResult)
		lc.logger.DebugContext(ctx, logger.CatApp, "llm classifier cache hit",
			"level", result.Level.String(),
		)
		return result, nil
	}

	// 2. Apply timeout
	classCtx, cancel := context.WithTimeout(ctx, lc.timeout)
	defer cancel()

	// 3. Build request (non-streaming, no thinking, compact output)
	req := agent.LLMRequest{
		Model:           lc.model,
		Temperature:     0, // deterministic
		MaxTokens:       llmClassifierMaxTokens,
		ThinkingEnabled: false,    // critical: no CoT for speed
		ResponseJSON:    true,     // force JSON output format
		ReasoningEffort: "",       // no reasoning
		Messages: []agent.LLMMessage{
			{Role: "system", Content: llmClassifierSystemPrompt},
			{Role: "user", Content: prompt},
		},
	}

	// 4. Synchronous call
	resp, err := lc.client.Chat(classCtx, req)
	if err != nil {
		// Timeout or API error → graceful degradation to L1
		lc.logger.DebugContext(ctx, logger.CatApp, "llm classifier failed, fallback to L1",
			"err", err.Error(),
		)
		return ClassificationResult{
			Level:      LevelSimpleSingleFile,
			Confidence: 50,
			Reason:     "LLM classifier timeout/error; defaulting to L1",
		}, nil // swallow error, return safe default
	}

	// 5. Parse JSON response
	result := parseLLMClassifyResponse(resp.Content)

	lc.logger.DebugContext(ctx, logger.CatApp, "llm classification complete",
		"level", result.Level.String(),
		"confidence", result.Confidence,
		"reason", result.Reason,
	)

	// 6. Cache and return
	lc.cache.Store(key, result)
	return result, nil
}

// ─── Response Parsing ────────────────────────────────────────────────────────

// llmClassifyJSON is the expected JSON structure from the LLM
type llmClassifyJSON struct {
	Intent string `json:"intent"` // "chat" or "action"
	Level  int    `json:"level"`  // 0, 1, 2, 3
	Reason string `json:"reason"`
}

// parseLLMClassifyResponse parses the LLM's JSON response into a ClassificationResult.
// Handles malformed responses gracefully (returns L1 default).
func parseLLMClassifyResponse(content string) ClassificationResult {
	// Strip potential markdown code block wrapping
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var raw llmClassifyJSON
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return ClassificationResult{
			Level:      LevelSimpleSingleFile,
			Confidence: 50,
			Reason:     "LLM response parse error; defaulting to L1",
		}
	}

	// Map raw level (0-3) to ClassificationLevel enum (1-4)
	var level ClassificationLevel
	switch raw.Level {
	case 0:
		level = LevelConversation
	case 1:
		level = LevelSimpleSingleFile
	case 2:
		level = LevelMediumMultiFile
	case 3:
		level = LevelComplexRefactoring
	default:
		level = LevelSimpleSingleFile // safety fallback
	}

	// LLM classification has fixed confidence of 80
	// (higher than low-confidence fast-track, lower than high-confidence fast-track)
	confidence := 80
	if raw.Intent == "chat" && level == LevelConversation {
		confidence = 90 // High confidence for clear conversation intent
	}

	reason := raw.Reason
	if reason == "" {
		reason = "LLM semantic classification"
	}

	return ClassificationResult{
		Level:            level,
		Confidence:       confidence,
		Reason:           reason,
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// hashPrompt returns a FNV-64 hash of the normalized prompt for cache lookup.
func hashPrompt(prompt string) uint64 {
	h := fnv.New64a()
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	h.Write([]byte(normalized))
	return h.Sum64()
}
