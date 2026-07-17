package wechat

import (
	"regexp"
	"strings"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	cjkItalicPattern     = regexp.MustCompile(`(^|[^*])\*([^*\n]*[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}][^*\n]*)\*([^*]|$)`)
)

// MarkdownFormatter adapts model Markdown to the subset rendered reliably by
// WeChat TEXT messages.
type MarkdownFormatter struct{}

func (MarkdownFormatter) FormatText(text string) string { return FormatText(text) }

// FormatText preserves common WeChat-compatible constructs and degrades syntax
// known to render poorly. Media links are removed because they must be sent as
// real media items rather than inline Markdown images.
func FormatText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = markdownImagePattern.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "~~", "")
		if strings.HasPrefix(line, "###### ") {
			line = strings.TrimPrefix(line, "###### ")
		} else if strings.HasPrefix(line, "##### ") {
			line = strings.TrimPrefix(line, "##### ")
		}
		line = strings.ReplaceAll(line, "***", "**")
		line = cjkItalicPattern.ReplaceAllString(line, "$1$2$3")
		lines[i] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
