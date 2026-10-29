package wechat

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import "testing"

func TestFormatTextKeepsCompatibleMarkdown(t *testing.T) {
	in := "**bold**\n`code`\n```go\n~~literal~~\n```\n| a | b |\n|---|---|"
	want := "**bold**\n`code`\n```go\n~~literal~~\n```\n| a | b |\n|---|---|"
	if got := FormatText(in); got != want {
		t.Fatalf("FormatText() = %q, want %q", got, want)
	}
}

func TestFormatTextDegradesUnsupportedMarkdown(t *testing.T) {
	in := testfixtures.Chinese("markdown_test_001")
	want := testfixtures.Chinese("markdown_test_002")
	if got := FormatText(in); got != want {
		t.Fatalf("FormatText() = %q, want %q", got, want)
	}
}
