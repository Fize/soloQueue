package simulation

import "strings"

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
