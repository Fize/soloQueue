package telegram

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	fencedCodeTelegramRE = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```")
	linkTelegramRE       = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	inlineCodeTelegramRE = regexp.MustCompile("`([^`]+)`")
	boldTelegramRE       = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	bulletTelegramRE     = regexp.MustCompile(`^(\s*)[-*+]\s+`)
)

// FormatText converts the common Markdown emitted by the model into Telegram
// HTML. HTML is used instead of MarkdownV2 because it requires less escaping
// for Chinese prose and has an explicit, small supported tag set.
func FormatText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	fences := make([]string, 0)
	text = fencedCodeTelegramRE.ReplaceAllStringFunc(text, func(match string) string {
		sub := fencedCodeTelegramRE.FindStringSubmatch(match)
		fences = append(fences, "<pre><code>"+html.EscapeString(sub[1])+"</code></pre>")
		return fmt.Sprintf("\x00FENCE%d\x00", len(fences)-1)
	})
	// Telegram has no table entity. Turn Markdown tables into a bold heading
	// followed by readable bullet rows before escaping and applying inline HTML.
	text = formatMarkdownTables(text)
	text = html.EscapeString(text)
	text = linkTelegramRE.ReplaceAllString(text, `<a href="$2">$1</a>`)
	text = inlineCodeTelegramRE.ReplaceAllString(text, `<code>$1</code>`)
	text = boldTelegramRE.ReplaceAllString(text, `<b>$1</b>`)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			line = strings.TrimLeft(line, "#")
			lines[i] = "<b>" + strings.TrimSpace(line) + "</b>"
			continue
		}
		if bulletTelegramRE.MatchString(line) {
			lines[i] = bulletTelegramRE.ReplaceAllString(line, "$1• ")
		}
	}
	text = strings.Join(lines, "\n")
	for i, fence := range fences {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00FENCE%d\x00", i), fence)
	}
	return strings.TrimSpace(text)
}

var tableSeparatorTelegramRE = regexp.MustCompile(`^:?-{3,}:?$`)

func formatMarkdownTables(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		header, ok := splitMarkdownTableRow(lines[i])
		if !ok || i+1 >= len(lines) {
			out = append(out, lines[i])
			continue
		}
		separator, separatorOK := splitMarkdownTableRow(lines[i+1])
		if !separatorOK || len(separator) != len(header) || !isMarkdownTableSeparator(separator) {
			out = append(out, lines[i])
			continue
		}

		out = append(out, "**"+strings.Join(header, " ｜ ")+"**")
		i++
		for i+1 < len(lines) {
			row, rowOK := splitMarkdownTableRow(lines[i+1])
			if !rowOK {
				break
			}
			out = append(out, "- "+strings.Join(row, " ｜ "))
			i++
		}
	}
	return strings.Join(out, "\n")
}

func splitMarkdownTableRow(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "|") {
		return nil, false
	}
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return nil, false
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, true
}

func isMarkdownTableSeparator(cells []string) bool {
	for _, cell := range cells {
		if !tableSeparatorTelegramRE.MatchString(cell) {
			return false
		}
	}
	return true
}

// SplitText splits by Unicode code points because Telegram's limit is measured
// in characters after entity parsing, not UTF-8 bytes.
func SplitText(text string, limit int) []string {
	if limit <= 0 {
		limit = 4096
	}
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)+limit-1)/limit)
	for len(runes) > 0 {
		n := limit
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}
