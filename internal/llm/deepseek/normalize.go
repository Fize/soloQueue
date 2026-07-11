package deepseek

import (
	"encoding/json"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

func normalizeMessages(msgs []agent.LLMMessage) []agent.LLMMessage {
	return normalize(msgs, true)
}

func normalize(msgs []agent.LLMMessage, dropOrphanTools bool) []agent.LLMMessage {
	if normalized, ok := tryNormalizeFastPath(msgs, dropOrphanTools); ok {
		return normalized
	}
	out := make([]agent.LLMMessage, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == "tool" {
				j++
			}
			calls := backfillToolCallNames(m.ToolCalls, msgs[i+1:j])
			m.ToolCalls = calls
			out = append(out, repairToolCallArgs(m))
			out = append(out, pairToolResults(calls, msgs[i+1:j])...)
			i = j
			continue
		}
		if m.Role == "tool" {
			if !dropOrphanTools {
				out = append(out, m)
			}
			i++
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

func tryNormalizeFastPath(msgs []agent.LLMMessage, dropOrphanTools bool) ([]agent.LLMMessage, bool) {
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == "tool" {
				j++
			}
			if !toolTurnWellFormed(m.ToolCalls, msgs[i+1:j]) || needsToolCallArgRepair(m.ToolCalls) {
				return nil, false
			}
			i = j
			continue
		}
		if m.Role == "tool" && dropOrphanTools {
			return nil, false
		}
		i++
	}
	return msgs, true
}

func toolTurnWellFormed(calls []llm.ToolCall, results []agent.LLMMessage) bool {
	if len(calls) != len(results) {
		return false
	}
	for _, tc := range calls {
		if tc.Function.Name == "" {
			return false
		}
	}
	for k, tc := range calls {
		if results[k].ToolCallID != tc.ID || results[k].Name != tc.Function.Name {
			return false
		}
	}
	return true
}

func needsToolCallArgRepair(calls []llm.ToolCall) bool {
	for _, tc := range calls {
		if tc.Function.Arguments != "" && !json.Valid([]byte(tc.Function.Arguments)) {
			return true
		}
	}
	return false
}

func repairToolCallArgs(m agent.LLMMessage) agent.LLMMessage {
	broken := false
	for _, tc := range m.ToolCalls {
		if tc.Function.Arguments != "" && !json.Valid([]byte(tc.Function.Arguments)) {
			broken = true
			break
		}
	}
	if !broken {
		return m
	}
	calls := make([]llm.ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if calls[i].Function.Arguments == "" || json.Valid([]byte(calls[i].Function.Arguments)) {
			continue
		}
		calls[i].Function.Arguments = closeTruncatedJSON(calls[i].Function.Arguments)
	}
	m.ToolCalls = calls
	return m
}

func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := s
	if esc {
		out = out[:len(out)-1]
	}
	if inStr {
		out += `"`
	}
	trimmed := strings.TrimRight(out, " \t\r\n")
	switch {
	case strings.HasSuffix(trimmed, ","):
		out = trimmed[:len(trimmed)-1]
	case strings.HasSuffix(trimmed, ":"):
		out = trimmed + "null"
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
	}
	if !json.Valid([]byte(out)) {
		return "{}"
	}
	return out
}

func pairToolResults(calls []llm.ToolCall, avail []agent.LLMMessage) []agent.LLMMessage {
	out := make([]agent.LLMMessage, 0, len(calls))
	if idDistinct(calls) {
		byID := make(map[string]agent.LLMMessage, len(avail))
		for _, r := range avail {
			byID[r.ToolCallID] = r
		}
		for _, tc := range calls {
			if r, ok := byID[tc.ID]; ok {
				r.Name = tc.Function.Name
				out = append(out, r)
			} else {
				out = append(out, agent.LLMMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    interruptedToolResult,
				})
			}
		}
		return out
	}
	for k, tc := range calls {
		if k < len(avail) {
			r := avail[k]
			r.ToolCallID = tc.ID
			r.Name = tc.Function.Name
			out = append(out, r)
		} else {
			out = append(out, agent.LLMMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    interruptedToolResult,
			})
		}
	}
	return out
}

func backfillToolCallNames(calls []llm.ToolCall, results []agent.LLMMessage) []llm.ToolCall {
	missing := false
	for _, c := range calls {
		if c.Function.Name == "" {
			missing = true
			break
		}
	}
	if !missing {
		return calls
	}
	out := make([]llm.ToolCall, len(calls))
	copy(out, calls)
	if idDistinct(calls) {
		byID := make(map[string]string, len(results))
		for _, r := range results {
			if r.Name != "" {
				byID[r.ToolCallID] = r.Name
			}
		}
		for k := range out {
			if out[k].Function.Name == "" {
				if n, ok := byID[out[k].ID]; ok {
					out[k].Function.Name = n
				}
			}
		}
		return out
	}
	for k := range out {
		if out[k].Function.Name == "" && k < len(results) {
			out[k].Function.Name = results[k].Name
		}
	}
	return out
}

func idDistinct(calls []llm.ToolCall) bool {
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		if tc.ID == "" {
			return false
		}
		if _, dup := seen[tc.ID]; dup {
			return false
		}
		seen[tc.ID] = struct{}{}
	}
	return true
}
