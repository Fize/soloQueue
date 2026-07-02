package ctxwin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Constants ───────────────────────────────────────────────────────────────────

const (
	// ephemeralTruncateThreshold is the minimum token count threshold to trigger middle-out truncation
	ephemeralTruncateThreshold = 1500

	// largeFieldTokenThreshold is the minimum token count threshold for a large field in a JSON object to trigger truncation
	largeFieldTokenThreshold = 500

	// minArrayElements is the minimum number of elements for a JSON array to trigger truncation
	minArrayElements = 10
)

// largeFields is a set of field names in JSON utility output that typically contain large blocks of text.
//
// The content of these fields is usually full file content / command output / HTTP body, suitable for truncation.
// Other fields (like exit_code, path, size, truncated, etc. metadata) are usually short and do not need truncation.
var largeFields = map[string]bool{
	"content":     true,
	"stdout":      true,
	"stderr":      true,
	"body":        true,
	"text":        true,
	"output":      true,
	"base64_data": true,
	"data":        true,
	"image":       true,
}

// ─── Step 1: Middle-Out Truncation ─────────────────────────────

// truncateMiddleOut scans all messages that are IsEphemeral and have Tokens > threshold,
// performing JSON-aware truncation or character-level truncation on their content.
//
// Returns true if any message was truncated.
func (cw *ContextWindow) truncateMiddleOut() bool {
	truncated := false
	truncatedCount := 0
	totalSavedTokens := 0

	for i := range cw.messages {
		msg := &cw.messages[i]
		if !msg.IsEphemeral || msg.Tokens <= ephemeralTruncateThreshold {
			continue
		}

		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "truncate_middle_out: ephemeral message detected",
				"msg_index", i,
				"msg_role", string(msg.Role),
				"msg_tokens", msg.Tokens,
				"threshold", ephemeralTruncateThreshold,
			)
		}

		newContent := tryJSONTruncate(msg.Content, cw.tokenizer)
		if newContent == "" {
			// JSON parsing failed or no truncation needed, fall back to character-level truncation
			newContent = charLevelTruncate(msg.Content, 0.10, 0.20)
		}
		msg.Content = newContent

		// Re-estimate tokens (quick estimation by character length to avoid BPE overhead on truncation path)
		// Calibrate will recalibrate with precise values on the next API call.
		oldTokens := msg.Tokens
		newTokens := cw.tokenizer.EstimateByLen(msg.Content) + cw.tokenizer.EstimateByLen(msg.ReasoningContent)
		savedTokens := oldTokens - newTokens
		cw.currentTokens -= savedTokens
		msg.Tokens = newTokens

		truncatedCount++
		totalSavedTokens += savedTokens

		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "truncate_middle_out: message truncated",
				"msg_index", i,
				"tokens_before", oldTokens,
				"tokens_after", newTokens,
				"tokens_saved", savedTokens,
				"content_len_before", len(msg.Content), // Note: this is after truncation, but shows result
			)
		}

		truncated = true
	}

	if truncated && cw.log != nil {
		cw.log.InfoContext(context.Background(), logger.CatMessages, "truncate_middle_out: completed",
			"messages_truncated", truncatedCount,
			"total_tokens_saved", totalSavedTokens,
			"remaining_messages", len(cw.messages),
		)
	}

	return truncated
}

// ─── JSON-aware Truncation ──────────────────────────────────────────────────────────

// tryJSONTruncate attempts "skeleton-preserving" truncation for JSON formatted content.
//
// Strategy:
//  1. First try JSON objects (map[string]any): truncate large string fields by keeping head and tail.
//  2. Next try JSON arrays ([]any): preserve head and tail elements, omit elements in between.
//  3. If both fail → return "", letting the caller proceed with character-level truncation.
//
// v1 Limitations:
//   - Large fields within nested objects will not be truncated (e.g., {"files": [{"content": "very long"}]},
//     `files` is an array, its internal `content` is not processed).
//   - If elements within large arrays are objects, the full object is preserved (no recursive truncation).
//   - In these scenarios, it falls back to character-level truncation, which DeepSeek usually tolerates.
func tryJSONTruncate(content string, tokenizer *Tokenizer) string {
	if result := tryJSONObjectTruncate(content, tokenizer); result != "" {
		return result
	}
	if result := tryJSONArrayTruncate(content, tokenizer); result != "" {
		return result
	}
	return ""
}

// tryJSONObjectTruncate processes JSON in the format {"key": "value", ...}.
//
// It truncates top-level large string fields (those in largeFields and with token count > largeFieldTokenThreshold)
// by keeping the head and tail, preserving the JSON structure.
// Returns "" if JSON parsing fails or no truncation is needed.
func tryJSONObjectTruncate(content string, tokenizer *Tokenizer) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return ""
	}

	modified := false
	for key, val := range obj {
		strVal, ok := val.(string)
		if !ok || !largeFields[key] {
			continue
		}
		if tokenizer.EstimateByLen(strVal) < largeFieldTokenThreshold {
			continue
		}
		obj[key] = charLevelTruncate(strVal, 0.10, 0.20)
		modified = true
	}

	if !modified {
		return ""
	}

	result, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(result)
}

// tryJSONArrayTruncate processes JSON in the format [item1, item2, ...].
//
// It retains the first 10% + last 20% of elements, replacing the middle with an omission marker string.
// v1 Limitation: If array elements are objects, the full object is preserved, no recursive truncation.
// Returns "" if JSON parsing fails, there are too few elements, or no truncation is needed.
func tryJSONArrayTruncate(content string, _ *Tokenizer) string {
	var arr []any
	if err := json.Unmarshal([]byte(content), &arr); err != nil {
		return ""
	}

	n := len(arr)
	if n < minArrayElements {
		return ""
	}

	headCount := max(1, int(float64(n)*0.10))
	tailCount := max(1, int(float64(n)*0.20))
	if headCount+tailCount >= n {
		return ""
	}

	result := make([]any, 0, headCount+1+tailCount)
	result = append(result, arr[:headCount]...)
	omitted := n - headCount - tailCount
	result = append(result, fmt.Sprintf("[...omitted %d elements...]", omitted))
	result = append(result, arr[n-tailCount:]...)

	b, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(b)
}

// ─── Character-level Truncation ─────────────────────────────────────────────────────────────

// charLevelTruncate truncates a string at the character level, keeping the head and tail.
//
// It retains characters from the first `headRatio` and last `tailRatio` proportions, replacing the middle with an omission marker.
// For cases where `headRatio + tailRatio >= 1.0`, the original string is returned.
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

// ─── Step 2: Turn-level FIFO Sliding Window ────────────────────────────────────────

// slideFIFO deletes the oldest messages in "conversation turns" until currentTokens <= targetTokens.
//
// Turn definition: all messages from a user message up to the next user message.
// i.e., Turn = [user, (assistant+tool_calls, tool, ..., assistant+tool_calls, tool)*, assistant]
//
// Guarantees:
//   - The system prompt (index 0) is never deleted.
//   - Each deletion removes a complete turn, ensuring the context always consists of complete "Q&A" sequences.
//   - No deletion occurs if only one turn remains (otherwise only the system prompt would remain, making the LLM unusable).
//   - When a turn is deleted, any subsequent orphan tool messages that reference `tool_call_ids` from the deleted turn are also cleaned up.
func (cw *ContextWindow) slideFIFO(targetTokens int) {
	turnsRemoved := 0
	totalTokensFreed := 0

	for cw.currentTokens > targetTokens {
		if len(cw.messages) <= 1 {
			if cw.log != nil {
				cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: cannot remove more turns",
					"reason", "only_system_prompt_left",
					"turns_removed", turnsRemoved,
					"total_tokens_freed", totalTokensFreed,
				)
			}
			break // Only system prompt left
		}
		turnEnd := cw.findTurnEnd(1)
		if turnEnd <= 1 {
			if cw.log != nil {
				cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: cannot remove more turns",
					"reason", "no_turn_found",
					"turns_removed", turnsRemoved,
					"total_tokens_freed", totalTokensFreed,
				)
			}
			break // No turn to delete
		}
		// If only one turn remains, the entire turn cannot be deleted (otherwise only the system prompt would remain, making the LLM unusable).
		// Instead, apply more aggressive truncation to messages within this turn.
		if turnEnd >= len(cw.messages) {
			aggressiveSaved := cw.aggressiveTruncateLastTurn(targetTokens)
			if aggressiveSaved > 0 {
				totalTokensFreed += aggressiveSaved
				if cw.log != nil {
					cw.log.InfoContext(context.Background(), logger.CatMessages, "slide_fifo: aggressive truncation applied to last turn",
						"tokens_saved", aggressiveSaved,
						"current_tokens", cw.currentTokens,
						"target_tokens", targetTokens,
					)
				}
				continue // Recheck capacity after truncation
			}
			if cw.log != nil {
				cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: cannot remove more turns",
					"reason", "only_one_turn_left",
					"turns_removed", turnsRemoved,
					"total_tokens_freed", totalTokensFreed,
				)
			}
			break
		}

		// Collect `tool_call_ids` from all assistant messages in the turn to be deleted
		// These will be used to clean up orphan tool messages in subsequent turns.
		orphanIDs := make(map[string]bool)
		for i := 1; i < turnEnd; i++ {
			for _, tc := range cw.messages[i].ToolCalls {
				orphanIDs[tc.ID] = true
			}
		}

		// Calculate tokens for the turn to be deleted
		removedTokens := 0
		removedCount := 0
		for i := 1; i < turnEnd; i++ {
			removedTokens += cw.messages[i].Tokens
			removedCount++
		}

		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: removing turn",
				"turn_index", turnsRemoved,
				"messages_in_turn", removedCount,
				"tokens_freed", removedTokens,
				"tokens_before", cw.currentTokens,
				"tokens_after", cw.currentTokens-removedTokens,
				"target_tokens", targetTokens,
			)
		}

		// Delete messages[1:turnEnd]
		cw.messages = append(cw.messages[:1], cw.messages[turnEnd:]...)
		cw.currentTokens -= removedTokens
		turnsRemoved++
		totalTokensFreed += removedTokens

		// Clean up orphan tool messages in subsequent turns (referencing `tool_call_id`s from the deleted assistant)
		if len(orphanIDs) > 0 {
			orphanRemoved := 0
			cleaned := cw.messages[:1]
			for i := 1; i < len(cw.messages); i++ {
				msg := cw.messages[i]
				if msg.Role == RoleTool && orphanIDs[msg.ToolCallID] {
					cw.currentTokens -= msg.Tokens
					totalTokensFreed += msg.Tokens
					orphanRemoved++
					continue
				}
				cleaned = append(cleaned, msg)
			}
			cw.messages = cleaned
			if cw.log != nil && orphanRemoved > 0 {
				cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: removed orphan tool messages",
					"count", orphanRemoved,
				)
			}
		}
	}

	if turnsRemoved > 0 && cw.log != nil {
		cw.log.InfoContext(context.Background(), logger.CatMessages, "slide_fifo: completed",
			"turns_removed", turnsRemoved,
			"total_tokens_freed", totalTokensFreed,
			"final_token_count", cw.currentTokens,
			"target_tokens", targetTokens,
			"remaining_messages", len(cw.messages),
		)
	}
}

// aggressiveTruncateLastTurn aggressively truncates messages within the last turn.
// It attempts in the following priority:
//   1. More aggressive head-tail truncation for IsEphemeral messages (keeping 5% head + 5% tail).
//   2. Aggressive truncation for non-ephemeral long messages (keeping 10% head + 10% tail).
//   3. If still exceeding, replace the content of the longest message with a placeholder "[content omitted]".
// Returns the actual number of tokens saved.
func (cw *ContextWindow) aggressiveTruncateLastTurn(targetTokens int) int {
	initialTokens := cw.currentTokens

	// Step 1: aggressively truncate ephemeral messages
	for i := 1; i < len(cw.messages); i++ {
		msg := &cw.messages[i]
		if !msg.IsEphemeral || msg.Tokens <= ephemeralTruncateThreshold {
			continue
		}
		newContent := charLevelTruncate(msg.Content, 0.05, 0.05)
		if newContent != msg.Content {
			oldTokens := msg.Tokens
			msg.Content = newContent
			msg.Tokens = cw.tokenizer.EstimateByLen(msg.Content) + cw.tokenizer.EstimateByLen(msg.ReasoningContent)
			if msg.Tokens < 1 {
				msg.Tokens = 1
			}
			cw.currentTokens -= oldTokens - msg.Tokens
			if cw.currentTokens < 0 {
				cw.currentTokens = 0
			}
		}
	}
	if cw.currentTokens <= targetTokens {
		return initialTokens - cw.currentTokens
	}

	// Step 2: aggressively truncate non-ephemeral long messages
	for i := 1; i < len(cw.messages); i++ {
		msg := &cw.messages[i]
		if msg.IsEphemeral || msg.Tokens <= ephemeralTruncateThreshold {
			continue
		}
		newContent := charLevelTruncate(msg.Content, 0.10, 0.10)
		if newContent != msg.Content {
			oldTokens := msg.Tokens
			msg.Content = newContent
			msg.Tokens = cw.tokenizer.EstimateByLen(msg.Content) + cw.tokenizer.EstimateByLen(msg.ReasoningContent)
			if msg.Tokens < 1 {
				msg.Tokens = 1
			}
			cw.currentTokens -= oldTokens - msg.Tokens
			if cw.currentTokens < 0 {
				cw.currentTokens = 0
			}
		}
	}
	if cw.currentTokens <= targetTokens {
		return initialTokens - cw.currentTokens
	}

	// Step 3: replace the longest remaining message with a placeholder
	maxIdx := -1
	maxTokens := 0
	for i := 1; i < len(cw.messages); i++ {
		if cw.messages[i].Tokens > maxTokens {
			maxTokens = cw.messages[i].Tokens
			maxIdx = i
		}
	}
	if maxIdx > 0 {
		msg := &cw.messages[maxIdx]
		oldTokens := msg.Tokens
		msg.Content = "[content omitted: too large to fit in context window]"
		msg.Tokens = cw.tokenizer.EstimateByLen(msg.Content) + cw.tokenizer.EstimateByLen(msg.ReasoningContent)
		if msg.Tokens < 1 {
			msg.Tokens = 1
		}
		cw.currentTokens -= oldTokens - msg.Tokens
		if cw.currentTokens < 0 {
			cw.currentTokens = 0
		}
	}

	return initialTokens - cw.currentTokens
}

// findTurnEnd finds the end position of the turn starting from `start`.
//
// A turn ends at the position of the next user message.
// If there is no next user message, it returns `len(messages)`.
func (cw *ContextWindow) findTurnEnd(start int) int {
	for i := start + 1; i < len(cw.messages); i++ {
		if cw.messages[i].Role == RoleUser {
			return i
		}
	}
	return len(cw.messages)
}

// pruneOlderTurnsEphemeralContent identifies the start boundary of protected turns (the index of the `protectTurns`-th user message from the end).
// For IsEphemeral messages before this boundary, it strips their large field content (if JSON, keys in `largeFields` are replaced with "[evicted]";
// otherwise, the entire content is replaced with "[evicted to save space]"), and updates the token count.
func (cw *ContextWindow) pruneOlderTurnsEphemeralContent(protectTurns int) {
	if len(cw.messages) <= 1 {
		return
	}

	// Find the boundary: the index of the protectTurns-th user message from the end.
	boundaryIdx := -1
	userCount := 0
	for i := len(cw.messages) - 1; i >= 0; i-- {
		if cw.messages[i].Role == RoleUser {
			userCount++
			if userCount == protectTurns {
				boundaryIdx = i
				break
			}
		}
	}

	// If there are not enough user turns, it means no older turns need pruning yet.
	if boundaryIdx < 0 {
		return
	}

	prunedCount := 0
	totalSavedTokens := 0

	for i := 0; i < boundaryIdx; i++ {
		msg := &cw.messages[i]
		if !msg.IsEphemeral || msg.Content == "" {
			continue
		}

		if msg.Content == "[evicted to save space]" {
			continue
		}

		oldTokens := msg.Tokens
		newContent := ""

		// Attempt to parse as a JSON object
		var obj map[string]any
		if err := json.Unmarshal([]byte(msg.Content), &obj); err == nil {
			modified := false
			for key := range obj {
				if largeFields[key] {
					if str, ok := obj[key].(string); ok && str == "[evicted]" {
						continue
					}
					obj[key] = "[evicted]"
					modified = true
				}
			}
			if modified {
				if b, err := json.Marshal(obj); err == nil {
					newContent = string(b)
				}
			}
		}

		if newContent == "" {
			// If not JSON or JSON processing failed, replace the entire content with a placeholder
			newContent = "[evicted to save space]"
		}

		if newContent != msg.Content {
			msg.Content = newContent
			msg.Tokens = cw.tokenizer.EstimateByLen(msg.Content) + cw.tokenizer.EstimateByLen(msg.ReasoningContent)
			if msg.Tokens < 1 {
				msg.Tokens = 1
			}
			savedTokens := oldTokens - msg.Tokens
			cw.currentTokens -= savedTokens
			prunedCount++
			totalSavedTokens += savedTokens
		}
	}

	if prunedCount > 0 && cw.log != nil {
		cw.log.InfoContext(context.Background(), logger.CatMessages, "pruneOlderTurnsEphemeralContent: completed",
			"messages_pruned", prunedCount,
			"total_tokens_saved", totalSavedTokens,
			"current_tokens", cw.currentTokens,
		)
	}
}