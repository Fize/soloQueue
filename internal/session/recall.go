package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// maxRecallChars is the maximum total character budget for the recalled
// memories block injected into the user prompt (~500 tokens). If the
// formatted result exceeds this, entries are truncated or dropped.
const maxRecallChars = 2000

// maxEntryLen is the maximum characters per individual recalled entry.
const maxEntryLen = 500

// buildRecalledContext searches the memory engine for context relevant to
// the user's prompt and returns a formatted text block to prepend to the
// prompt. Returns empty string when no relevant memories are found or when
// the memory engine is disabled.
//
// The returned block uses a lightweight XML/markdown hybrid format that's
// easy for the LLM to parse and doesn't add significant token overhead.
func (s *Session) buildRecalledContext(ctx context.Context, prompt string) string {
	if s.memoryEngine == nil {
		return ""
	}

	// Skip if prompt is very short — not enough signal for useful recall.
	trimmed := strings.TrimSpace(prompt)
	if len(trimmed) < 10 {
		return ""
	}

	// Use the full user prompt as the search query. BM25 handles the
	// keyword extraction internally via FTS5 tokenization — no need for
	// separate NLP preprocessing.
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	results, err := s.memoryEngine.Search(ctxTimeout, memoryengine.SearchQuery{
		Text:  trimmed,
		Limit: 5,
	})
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "preload recall: search failed",
				"err", err.Error(),
				"prompt_len", len(trimmed),
			)
		}
		return ""
	}

	if len(results.Results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<recalled_memories>\n")

	budget := maxRecallChars
	used := 0

	for i, r := range results.Results {
		// Format date for display
		date := r.Date
		if date == "" {
			date = r.EventTime
			// Trim time portion if present (ISO 8601 → date only)
			if len(date) > 10 {
				date = date[:10]
			}
		}

		content := strings.TrimSpace(r.Content)
		if len(content) > maxEntryLen {
			content = content[:maxEntryLen] + "..."
		}

		line := fmt.Sprintf("%d. [%s] %s\n", i+1, date, content)
		lineLen := len(line)

		if used+lineLen > budget {
			// Try to fit a truncated version of this line
			remaining := budget - used
			if remaining > 30 {
				shortContent := r.Content
				if len(shortContent) > remaining-30 {
					shortContent = shortContent[:remaining-30] + "..."
				}
				shortLine := fmt.Sprintf("%d. [%s] %s\n", i+1, date, shortContent)
				b.WriteString(shortLine)
			}
			break
		}

		b.WriteString(line)
		used += lineLen
	}

	b.WriteString("</recalled_memories>")
	return b.String()
}
