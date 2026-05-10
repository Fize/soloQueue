package qqbot

import (
	"regexp"
	"strings"
)

// ─── QQ Markdown Converter ───────────────────────────────────────────────────

var (
	// Matches fenced code blocks: ```lang\n...\n```
	fencedBlockRE = regexp.MustCompile("(?s)```([^\n]*)\n(.*?)```")

	// Matches inline code: `text` (not preceded or followed by backtick)
	inlineCodeRE = regexp.MustCompile("`([^`]+)`")

	// Matches a likely table row (starts and ends with |, contains | separators)
	tableRowRE = regexp.MustCompile(`^\|.*\|$`)
)

// QQMarkdown converts LLM output (CommonMark) to QQ-compatible markdown.
//
// Transformations:
//  1. Fenced code blocks → bold header + indented plain body
//  2. Inline code → bold
//  3. Tables → bulleted lists
//
// Preserved: headings, bold, italic, strikethrough, links, images, lists,
// block quotes, horizontal rules.
func QQMarkdown(input string) string {
	if input == "" {
		return input
	}

	// Step 1: Replace fenced code blocks.
	// Must be done first to avoid matching ``` inside transformed content.
	result := fencedBlockRE.ReplaceAllStringFunc(input, func(match string) string {
		sub := fencedBlockRE.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		lang := strings.TrimSpace(sub[1])
		body := sub[2]

		var b strings.Builder
		if lang != "" {
			b.WriteString("**" + lang + "**\n")
		} else {
			b.WriteString("**Code**\n")
		}
		// Indent body lines with 2 spaces
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		return b.String()
	})

	// Step 2: Replace inline code with bold.
	// Escape ** inside inline code to avoid broken bold rendering.
	result = inlineCodeRE.ReplaceAllStringFunc(result, func(match string) string {
		inner := inlineCodeRE.FindStringSubmatch(match)[1]
		// Escape any ** inside the code to prevent broken bold
		escaped := strings.ReplaceAll(inner, "**", "\\*\\*")
		return "**" + escaped + "**"
	})

	// Step 3: Detect and convert tables to lists.
	// A table is: 2+ consecutive lines that all match table row pattern.
	lines := strings.Split(result, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		if tableRowRE.MatchString(lines[i]) {
			// Collect consecutive table lines
			start := i
			for i < len(lines) && tableRowRE.MatchString(lines[i]) {
				i++
			}
			tableLines := lines[start:i]
			out = append(out, convertTableRows(tableLines)...)
		} else {
			out = append(out, lines[i])
			i++
		}
	}

	return strings.Join(out, "\n")
}

// convertTableRows converts table lines into a bulleted list.
// The first row is treated as a header (bold), separator rows are skipped.
func convertTableRows(lines []string) []string {
	var result []string
	added := false

	for _, line := range lines {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		trimmed := make([]string, 0, len(cells))
		for _, c := range cells {
			t := strings.TrimSpace(c)
			if t != "" {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) == 0 {
			continue
		}

		// Check if this is a separator row (e.g., |---|---|)
		isSep := true
		for _, c := range trimmed {
			c = strings.TrimSpace(c)
			if !strings.ContainsAny(c, "-:") && c != "" {
				// If cell has content that's not just dashes/colons, it's not a pure sep row
				isSep = false
				break
			}
			// Empty cell is also suspicious for non-sep
			if c == "" {
				isSep = false
			}
		}
		if isSep && len(trimmed) > 0 {
			continue // skip separator rows
		}

		if !added {
			// First data row as bold header
			result = append(result, "- **"+strings.Join(trimmed, ": ")+"**")
			added = true
		} else {
			result = append(result, "- "+strings.Join(trimmed, ": "))
		}
	}
	return result
}

// ─── Markdown-Aware Splitting ────────────────────────────────────────────────

// SplitMarkdown splits markdown text into chunks no larger than maxLen bytes.
// It prefers to split at heading boundaries (# / ##), then paragraph breaks,
// then line breaks, and finally hard-splits if a single section is too large.
func SplitMarkdown(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	// Try splitting at heading boundaries (greedy section accumulation).
	chunks := splitSections(text, maxLen)
	if len(chunks) > 1 {
		return finalizeChunks(chunks, maxLen)
	}

	// Try splitting at paragraph boundaries.
	chunks = splitSections(text, maxLen) // will return single-element; try paragraph
	chunks = splitAtParagraphs(text, maxLen)
	if len(chunks) > 1 {
		return finalizeChunks(chunks, maxLen)
	}

	// Fall back to line-based splitting.
	return splitAtLines(text, maxLen)
}

// splitSections splits text into heading-delimited sections, then greedily
// accumulates sections into chunks. Each section starts with a # or ## heading.
func splitSections(text string, maxLen int) []string {
	headingRE := regexp.MustCompile(`(?m)^#{1,2}\s`)
	locs := headingRE.FindAllStringIndex(text, -1)
	if len(locs) < 2 {
		return []string{text}
	}

	// Extract sections: each spans from its heading to just before the next heading.
	sections := make([]string, len(locs))
	for i := 0; i < len(locs); i++ {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		sections[i] = text[locs[i][0]:end]
	}

	// Greedily accumulate sections into chunks.
	var chunks []string
	current := ""
	for _, sec := range sections {
		candidate := current + sec
		if len(candidate) > maxLen && current != "" {
			chunks = append(chunks, strings.TrimSpace(current))
			current = sec
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, strings.TrimSpace(current))
	}
	if len(chunks) <= 1 {
		return []string{text}
	}
	return chunks
}

// splitAtParagraphs splits text at double-newline boundaries.
func splitAtParagraphs(text string, maxLen int) []string {
	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) < 2 {
		return []string{text}
	}

	var chunks []string
	current := ""
	for _, para := range paragraphs {
		candidate := current
		if candidate != "" {
			candidate += "\n\n"
		}
		candidate += para
		if len(candidate) > maxLen && current != "" {
			chunks = append(chunks, strings.TrimSpace(current))
			current = para
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, strings.TrimSpace(current))
	}
	if len(chunks) <= 1 {
		return []string{text}
	}
	return chunks
}

// splitAtLines is the fallback: split at newlines, then hard-split if needed.
func splitAtLines(text string, maxLen int) []string {
	var chunks []string
	for len(text) > maxLen {
		splitAt := maxLen
		idx := strings.LastIndex(text[:maxLen], "\n")
		if idx > maxLen/2 {
			splitAt = idx + 1
		}
		chunks = append(chunks, text[:splitAt])
		text = text[splitAt:]
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}

// finalizeChunks ensures no individual chunk exceeds maxLen.
// Oversized chunks are re-split at line boundaries (never paragraph boundaries,
// as that would break heading-content association).
func finalizeChunks(chunks []string, maxLen int) []string {
	var result []string
	for _, c := range chunks {
		if len(c) <= maxLen {
			result = append(result, c)
			continue
		}
		// Only use line-split for oversized single chunks to preserve semantics.
		result = append(result, splitAtLines(c, maxLen)...)
	}
	if len(result) == 0 {
		return chunks
	}
	return result
}
