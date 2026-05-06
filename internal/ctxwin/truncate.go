package ctxwin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── 常量 ───────────────────────────────────────────────────────────────────

const (
	// ephemeralTruncateThreshold 是触发中间截断的最低 token 数阈值
	ephemeralTruncateThreshold = 1500

	// largeFieldTokenThreshold 是 JSON 对象中大字段触发截断的最低 token 数阈值
	largeFieldTokenThreshold = 500

	// minArrayElements 是 JSON 数组触发截断的最低元素数阈值
	minArrayElements = 10
)

// largeFields 是 JSON 工具输出中通常包含大段文本的字段名集合
//
// 这些字段的内容通常是文件全文/命令输出/HTTP body，适合截断。
// 其他字段（如 exit_code, path, size, truncated 等元数据）通常很短，不需要截断。
var largeFields = map[string]bool{
	"content": true,
	"stdout":  true,
	"stderr":  true,
	"body":    true,
	"text":    true,
	"output":  true,
}

// ─── Step 1: 中间截断法 (Middle-Out Truncation) ─────────────────────────────

// truncateMiddleOut 扫描所有 IsEphemeral 且 Tokens > 阈值的消息，
// 对其内容执行 JSON 感知截断或字符级截断
//
// 返回 true 如果有任何消息被截断。
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
			// JSON 解析失败或無需截斷，回退到字符級截斷
			newContent = charLevelTruncate(msg.Content, 0.10, 0.20)
		}
		msg.Content = newContent

		// 重新計算 token（包含 Content + ReasoningContent）
		oldTokens := msg.Tokens
		newTokens := cw.tokenizer.Count(msg.Content) + cw.tokenizer.Count(msg.ReasoningContent)
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

// ─── JSON 感知截断 ──────────────────────────────────────────────────────────

// tryJSONTruncate 尝试对 JSON 格式的内容做"骨架保留"截断
//
// 策略：
//  1. 优先尝试 JSON 对象 (map[string]any)：对大字符串字段做掐头去尾
//  2. 其次尝试 JSON 数组 ([]any)：保留头尾元素，中间省略
//  3. 都失败 → 返回 ""，由调用方走字符级截断
//
// v1 限制：
//   - 嵌套对象内的大字段不会被截断（如 {"files": [{"content": "超长"}]}，
//     files 是数组，其内部 content 不被处理）
//   - 大数组内的元素如果是对象，保留的是完整对象（不做递归截断）
//   - 这些场景下回退到字符级截断，DeepSeek 通常能容错理解
func tryJSONTruncate(content string, tokenizer *Tokenizer) string {
	if result := tryJSONObjectTruncate(content, tokenizer); result != "" {
		return result
	}
	if result := tryJSONArrayTruncate(content, tokenizer); result != "" {
		return result
	}
	return ""
}

// tryJSONObjectTruncate 处理 {"key": "value", ...} 格式的 JSON
//
// 对顶层大字符串字段（在 largeFields 中且 token 数 > largeFieldTokenThreshold）
// 执行掐头去尾，保留 JSON 骨架。
// 返回 "" 表示 JSON 解析失败或无需截断。
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
		if tokenizer.Count(strVal) < largeFieldTokenThreshold {
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

// tryJSONArrayTruncate 处理 [item1, item2, ...] 格式的 JSON
//
// 保留前 10% + 后 20% 的元素，中间替换为省略标记字符串。
// v1 限制：数组元素如果是对象，保留完整对象，不做递归截断。
// 返回 "" 表示 JSON 解析失败、元素太少或无需截断。
func tryJSONArrayTruncate(content string, tokenizer *Tokenizer) string {
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

// ─── 字符级截断 ─────────────────────────────────────────────────────────────

// charLevelTruncate 对字符串做字符级掐头去尾
//
// 保留前 headRatio 和后 tailRatio 比例的字符，中间替换为省略标记。
// 对于 headRatio + tailRatio >= 1.0 的情况，原样返回。
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

// ─── Step 2: Turn 粒度 FIFO 滑动窗口 ────────────────────────────────────────

// slideFIFO 以"对话轮次 (Turn)"为单位删除最老的消息，直到 currentTokens <= targetTokens
//
// Turn 定义：从 user 消息开始，到下一个 user 消息之前的所有消息。
// 即：Turn = [user, (assistant+tool_calls, tool, ..., assistant+tool_calls, tool)*, assistant]
//
// 保证：
//   - system prompt（索引 0）永远不被删除
//   - 每次删除一个完整 Turn，保证上下文始终是完整的"问答对"序列
//   - 只剩一个 Turn 时不删除（否则只剩 system prompt，LLM 无法工作）
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
			break // 只剩 system prompt
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
			break // 没有 Turn 可删
		}
		// 只剩一个 Turn 时，不删除
		if turnEnd >= len(cw.messages) {
			if cw.log != nil {
				cw.log.DebugContext(context.Background(), logger.CatMessages, "slide_fifo: cannot remove more turns",
					"reason", "only_one_turn_left",
					"turns_removed", turnsRemoved,
					"total_tokens_freed", totalTokensFreed,
				)
			}
			break
		}
		// 删除 messages[1:turnEnd]
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
				"tokens_after", cw.currentTokens - removedTokens,
				"target_tokens", targetTokens,
			)
		}

		cw.messages = append(cw.messages[:1], cw.messages[turnEnd:]...)
		cw.currentTokens -= removedTokens
		turnsRemoved++
		totalTokensFreed += removedTokens
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

// findTurnEnd 找到从 start 开始的 Turn 的结束位置
//
// Turn 结束于下一个 user 消息的位置。
// 如果没有下一个 user 消息，返回 len(messages)。
func (cw *ContextWindow) findTurnEnd(start int) int {
	for i := start + 1; i < len(cw.messages); i++ {
		if cw.messages[i].Role == RoleUser {
			return i
		}
	}
	return len(cw.messages)
}


