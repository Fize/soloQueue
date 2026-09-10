package telegram

import (
	"strings"
	"testing"
)

func TestFormatTextConvertsCommonMarkdown(t *testing.T) {
	got := FormatText("**万华** [看板](http://127.0.0.1:19876)")
	if !strings.Contains(got, "<b>万华</b>") || !strings.Contains(got, "<a href=") || !strings.Contains(got, ">看板</a>") {
		t.Fatalf("FormatText() = %q", got)
	}
}

func TestFormatTextEscapesHTMLAndPreservesCode(t *testing.T) {
	got := FormatText("```go\nif a < b {\n}\n```")
	want := "<pre><code>if a &lt; b {\n}\n</code></pre>"
	if got != want {
		t.Fatalf("FormatText() = %q, want %q", got, want)
	}
}

func TestFormatTextRendersMarkdownTableAsBullets(t *testing.T) {
	got := FormatText("| 茶 | 为什么 |\n|---|---|\n| 山楂水 | 传统消食第一名 |\n| 陈皮水 | 理气 |\n")
	if strings.Contains(got, "|---|") || strings.Contains(got, "| 山楂水 |") {
		t.Fatalf("FormatText() kept Markdown table syntax: %q", got)
	}
	if !strings.Contains(got, "<b>茶 ｜ 为什么</b>") || !strings.Contains(got, "• 山楂水 ｜ 传统消食第一名") {
		t.Fatalf("FormatText() = %q", got)
	}
}

func TestSplitTextUsesUnicodeCharacters(t *testing.T) {
	parts := SplitText("你好世界", 3)
	if len(parts) != 2 || parts[0] != "你好世" || parts[1] != "界" {
		t.Fatalf("parts = %#v", parts)
	}
}
