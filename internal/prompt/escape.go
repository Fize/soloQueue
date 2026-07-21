package prompt

import "strings"

// escapePromptData prevents dynamic data from opening or closing the XML-like
// sections used by the system prompt while keeping Markdown readable.
func escapePromptData(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}
