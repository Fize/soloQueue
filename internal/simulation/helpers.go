package simulation

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func cleanJSONResponse(content string) string {
	cleaned := strings.TrimSpace(content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	return escapeControlCharsInStrings(cleaned)
}

// escapeControlCharsInStrings fixes a common LLM JSON mistake: raw control
// characters (newline, tab, carriage return, etc.) inside JSON string values.
// The JSON spec (RFC 8259) requires all control characters U+0000–U+001F to be
// escaped inside strings, but some LLMs emit literal newlines in fields like
// "description" or "reason". Go's json.Unmarshal rejects these with
// "invalid character '\n' in string literal".
//
// This function scans the input byte-by-byte, tracks whether the current
// position is inside a string literal (between unescaped double quotes), and
// replaces any raw control character with its escaped form (\n, \t, \r, etc.).
// Control characters outside string literals (between tokens) are valid JSON
// whitespace and are left untouched.
func escapeControlCharsInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 32)

	inString := false
	escaped := false // true if previous char was a backslash

	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)
			continue
		}

		// Inside a string literal
		if escaped {
			// Previous char was '\', so this char is part of an escape sequence
			b.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			b.WriteByte(c)
			inString = false
			continue
		}

		// Check for control characters that need escaping
		if c < 0x20 {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				// Other control chars: use \uXXXX
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			}
			continue
		}

		b.WriteByte(c)
	}

	return b.String()
}

// chatWithJSONRetry is a shared helper that calls the LLM, tries to parse the
// JSON response, and retries once with a fix instruction on parse failure.
// parseFn validates the JSON content; retryNote adds provider-specific hints
// to the retry prompt. logTruncation controls FinishLength warnings.
func chatWithJSONRetry(
	ctx context.Context,
	llmClient agent.LLMClient,
	model, providerID string,
	log *logger.Logger,
	prompt string,
	maxTokens int,
	parseFn func(string) error,
	retryNote string,
	logTruncation bool,
) (string, error) {
	resp, err := llmClient.Chat(ctx, agent.LLMRequest{
		Model:        model,
		ProviderID:   providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if err != nil {
		return "", err
	}

	if logTruncation && resp.FinishReason == llm.FinishLength {
		if log != nil {
			log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: LLM response truncated",
				"content_len", len(resp.Content))
		}
	}

	parseErr := parseFn(resp.Content)
	if parseErr == nil {
		return resp.Content, nil
	}

	if log != nil {
		log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: first parse failed, retrying",
			"err", parseErr.Error())
	}

	retryPrompt := prompt + fmt.Sprintf("\n\n[SYSTEM] Your previous JSON response was invalid: %s\nPlease fix the JSON syntax and output ONLY valid JSON. %s\n",
		parseErr.Error(), retryNote)

	retryResp, retryErr := llmClient.Chat(ctx, agent.LLMRequest{
		Model:        model,
		ProviderID:   providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: retryPrompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if retryErr != nil {
		return "", fmt.Errorf("retry after parse error: %w (original: %w)", retryErr, parseErr)
	}

	if logTruncation && retryResp.FinishReason == llm.FinishLength {
		if log != nil {
			log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: retry response truncated",
				"content_len", len(retryResp.Content))
		}
	}

	return retryResp.Content, nil
}
