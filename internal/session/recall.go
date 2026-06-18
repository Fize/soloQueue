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

	s.mu.Lock()
	defer s.mu.Unlock()

	// Filter out results already seen in this session context window
	var targets []memoryengine.SearchResult
	for _, r := range results.Results {
		if _, ok := s.recalledHashes[r.ContentHash]; !ok {
			targets = append(targets, r)
		}
	}

	if len(targets) == 0 {
		return ""
	}

	// Compute dates, age labels, and if any are stale
	type processedEntry struct {
		result   memoryengine.SearchResult
		date     string
		ageLabel string
		isStale  bool
	}

	var processed []processedEntry
	anyStale := false

	now := time.Now()
	todayStr := now.Format("2006-01-02")
	today, _ := time.ParseInLocation("2006-01-02", todayStr, now.Location())

	for _, r := range targets {
		date := r.Date
		if date == "" {
			date = r.EventTime
			if len(date) > 10 {
				date = date[:10]
			}
		}

		var ageLabel string
		isStale := false
		if date != "" {
			memTime, err := time.ParseInLocation("2006-01-02", date, now.Location())
			if err == nil {
				days := int(today.Sub(memTime).Hours() / 24)
				if days <= 0 {
					ageLabel = "today"
				} else if days == 1 {
					ageLabel = "1d ago"
				} else if days <= 7 {
					ageLabel = fmt.Sprintf("%dd ago", days)
				} else {
					ageLabel = fmt.Sprintf("stale %dd", days)
					isStale = true
					anyStale = true
				}
			}
		}

		processed = append(processed, processedEntry{
			result:   r,
			date:     date,
			ageLabel: ageLabel,
			isStale:  isStale,
		})
	}

	var b strings.Builder
	b.WriteString("<recalled_memories>\n")

	budget := maxRecallChars
	used := len("<recalled_memories>\n</recalled_memories>")

	const staleWarning = "⚠️ Memories marked [stale] are >7 days old and may be outdated — verify before presenting as fact.\n"
	if anyStale {
		b.WriteString(staleWarning)
		used += len(staleWarning)
	}

	for i, p := range processed {
		content := strings.TrimSpace(p.result.Content)
		if len(content) > maxEntryLen {
			content = content[:maxEntryLen] + "..."
		}

		var line string
		if p.ageLabel != "" {
			line = fmt.Sprintf("%d. [%s, %s] %s\n", i+1, p.date, p.ageLabel, content)
		} else {
			line = fmt.Sprintf("%d. [%s] %s\n", i+1, p.date, content)
		}
		lineLen := len(line)

		if used+lineLen > budget {
			// Try to fit a truncated version of this line
			remaining := budget - used
			if remaining > 30 {
				shortContent := p.result.Content
				if len(shortContent) > remaining-30 {
					shortContent = shortContent[:remaining-30] + "..."
				}
				var shortLine string
				if p.ageLabel != "" {
					shortLine = fmt.Sprintf("%d. [%s, %s] %s\n", i+1, p.date, p.ageLabel, shortContent)
				} else {
					shortLine = fmt.Sprintf("%d. [%s] %s\n", i+1, p.date, shortContent)
				}
				b.WriteString(shortLine)
				s.recalledHashes[p.result.ContentHash] = struct{}{}
			}
			break
		}

		b.WriteString(line)
		used += lineLen
		s.recalledHashes[p.result.ContentHash] = struct{}{}
	}

	b.WriteString("</recalled_memories>")
	return b.String()
}
