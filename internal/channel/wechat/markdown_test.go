package wechat

import "testing"

func TestFormatTextKeepsCompatibleMarkdown(t *testing.T) {
	in := "**bold**\n`code`\n```go\n~~literal~~\n```\n| a | b |\n|---|---|"
	want := "**bold**\n`code`\n```go\n~~literal~~\n```\n| a | b |\n|---|---|"
	if got := FormatText(in); got != want {
		t.Fatalf("FormatText() = %q, want %q", got, want)
	}
}

func TestFormatTextDegradesUnsupportedMarkdown(t *testing.T) {
	in := "##### title\n*中文斜体*\n~~deleted~~\n![image](https://example.test/a.png)"
	want := "title\n中文斜体\ndeleted"
	if got := FormatText(in); got != want {
		t.Fatalf("FormatText() = %q, want %q", got, want)
	}
}
