package qqbot

import (
	"strings"
	"testing"
)

// ─── QQMarkdown tests ────────────────────────────────────────────────────────

func TestQQMarkdown_Empty(t *testing.T) {
	if got := QQMarkdown(""); got != "" {
		t.Errorf("QQMarkdown() = %q, want empty", got)
	}
}

func TestQQMarkdown_PlainText(t *testing.T) {
	input := "Hello world, this is plain text."
	got := QQMarkdown(input)
	if got != input {
		t.Errorf("QQMarkdown() = %q, want %q", got, input)
	}
}

func TestQQMarkdown_HeadingsPreserved(t *testing.T) {
	input := "# H1 Heading\n\n## H2 Heading\n\nSome text."
	got := QQMarkdown(input)
	if !strings.Contains(got, "# H1 Heading") || !strings.Contains(got, "## H2 Heading") {
		t.Errorf("headings not preserved: %q", got)
	}
}

func TestQQMarkdown_BoldPreserved(t *testing.T) {
	input := "This is **bold text** here."
	got := QQMarkdown(input)
	if got != input {
		t.Errorf("bold not preserved: %q", got)
	}
}

func TestQQMarkdown_ItalicPreserved(t *testing.T) {
	input := "This is *italic* and _also italic_."
	got := QQMarkdown(input)
	if got != input {
		t.Errorf("italic not preserved: %q", got)
	}
}

func TestQQMarkdown_StrikethroughPreserved(t *testing.T) {
	input := "This is ~~strikethrough~~ text."
	got := QQMarkdown(input)
	if got != input {
		t.Errorf("strikethrough not preserved: %q", got)
	}
}

func TestQQMarkdown_LinkPreserved(t *testing.T) {
	input := "Visit [this](https://example.com) link."
	got := QQMarkdown(input)
	if got != input {
		t.Errorf("link not preserved: %q", got)
	}
}

func TestQQMarkdown_InlineCode(t *testing.T) {
	input := "Use `fmt.Println()` to print."
	got := QQMarkdown(input)
	want := "Use **fmt.Println()** to print."
	if got != want {
		t.Errorf("QQMarkdown() = %q, want %q", got, want)
	}
}

func TestQQMarkdown_InlineCodeWithAsterisks(t *testing.T) {
	input := "Use `**bold**` inline."
	got := QQMarkdown(input)
	// Asterisks inside inline code should be escaped
	want := "Use **\\*\\*bold\\*\\*** inline."
	if got != want {
		t.Errorf("QQMarkdown() = %q, want %q", got, want)
	}
}

func TestQQMarkdown_FencedBlockNoLang(t *testing.T) {
	input := "Before\n```\nline1\nline2\n```\nAfter"
	got := QQMarkdown(input)
	if !strings.Contains(got, "**Code**") {
		t.Error("fenced block without lang should have '**Code**' header")
	}
	if !strings.Contains(got, "  line1") || !strings.Contains(got, "  line2") {
		t.Error("fenced block body should be indented")
	}
	if strings.Contains(got, "```") {
		t.Error("fenced block markers should be removed")
	}
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After") {
		t.Error("surrounding text should be preserved")
	}
}

func TestQQMarkdown_FencedBlockWithLang(t *testing.T) {
	input := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	got := QQMarkdown(input)
	if !strings.Contains(got, "**go**") {
		t.Error("fenced block with lang should have '**go**' header")
	}
	if !strings.Contains(got, "  func main()") {
		t.Error("fenced block body should be indented and preserved")
	}
}

func TestQQMarkdown_MultipleFencedBlocks(t *testing.T) {
	input := "```python\nprint(1)\n```\n\n```go\nfmt.Println(2)\n```"
	got := QQMarkdown(input)
	if !strings.Contains(got, "**python**") || !strings.Contains(got, "**go**") {
		t.Error("multiple fenced blocks should each have headers")
	}
}

func TestQQMarkdown_TableConversion(t *testing.T) {
	input := "| Name | Age |\n|------|-----|\n| John | 30  |\n| Jane | 25  |"
	got := QQMarkdown(input)
	if strings.Contains(got, "|") {
		t.Error("table pipes should be removed")
	}
	if !strings.Contains(got, "- **") {
		t.Error("first header row should be bold")
	}
	if !strings.Contains(got, "John") || !strings.Contains(got, "Jane") {
		t.Error("table data should be preserved")
	}
}

func TestQQMarkdown_MixedContent(t *testing.T) {
	input := "# Summary\n\nHere is `some code` inline.\n\n```go\nx := 1\n```\n\n**Bold** and *italic* text."
	got := QQMarkdown(input)
	if strings.Contains(got, "`") {
		t.Error("backticks should be removed")
	}
	if strings.Contains(got, "```") {
		t.Error("triple backticks should be removed")
	}
	if !strings.Contains(got, "# Summary") {
		t.Error("heading should be preserved")
	}
	if !strings.Contains(got, "**some code**") {
		t.Error("inline code should become bold")
	}
	if !strings.Contains(got, "**go**") {
		t.Error("fenced block should have lang header")
	}
	if !strings.Contains(got, "**Bold**") {
		t.Error("bold should be preserved")
	}
	if !strings.Contains(got, "*italic*") {
		t.Error("italic should be preserved")
	}
}

// ─── SplitMarkdown tests ─────────────────────────────────────────────────────

func TestSplitMarkdown_UnderLimit(t *testing.T) {
	text := "short message"
	chunks := SplitMarkdown(text, 100)
	if len(chunks) != 1 || chunks[0] != text {
		t.Errorf("short text should stay as one chunk: got %v", chunks)
	}
}

func TestSplitMarkdown_AtHeadings(t *testing.T) {
	text := ""
	for i := 0; i < 3; i++ {
		text += "## Section " + string(rune('A'+i)) + "\n\n"
		text += strings.Repeat("Content line here.\n", 20)
	}
	chunks := SplitMarkdown(text, 500)
	if len(chunks) <= 1 {
		t.Errorf("long text with headings should be split: got %d chunks", len(chunks))
	}
	// Verify each chunk starts with a heading
	for _, c := range chunks {
		if !strings.HasPrefix(strings.TrimSpace(c), "##") {
			t.Errorf("chunk should start with heading: %q", c[:50])
		}
	}
}

func TestSplitMarkdown_AtParagraphs(t *testing.T) {
	text := strings.Repeat("A fairly long paragraph that contains a lot of text.\n", 30)
	text += "\n\n"
	text += strings.Repeat("Another long paragraph here.\n", 30)
	chunks := SplitMarkdown(text, 500)
	if len(chunks) <= 1 {
		t.Errorf("long text with paragraphs should be split: got %d chunks", len(chunks))
	}
}

func TestSplitMarkdown_LineFallback(t *testing.T) {
	// Single long line with no headings or paragraph breaks
	text := strings.Repeat("abcdefghij", 200) // 2000 chars, no breaks
	chunks := SplitMarkdown(text, 500)
	if len(chunks) <= 1 {
		t.Errorf("long unbroken text should be hard-split: got %d chunks", len(chunks))
	}
	// Reassembled text should match original
	joined := strings.Join(chunks, "")
	if joined != text {
		t.Errorf("reassembled text mismatch: len(joined)=%d, len(original)=%d", len(joined), len(text))
	}
}

// ─── MessageReq JSON serialization tests ─────────────────────────────────────

func TestMessageReq_TextJSON(t *testing.T) {
	req := buildMessageReq(MsgTypeText, "hello", "msg-1", 1)
	if req.Content != "hello" {
		t.Error("text message should have Content field")
	}
	if req.Markdown != nil {
		t.Error("text message should not have Markdown field")
	}
	if req.MsgType != MsgTypeText {
		t.Error("wrong msg_type")
	}
}

func TestMessageReq_MarkdownJSON(t *testing.T) {
	req := buildMessageReq(MsgTypeMarkdown, "# Hello", "msg-2", 2)
	if req.Content != "" {
		t.Error("markdown message should NOT have flat Content field")
	}
	if req.Markdown == nil || req.Markdown.Content != "# Hello" {
		t.Error("markdown content missing")
	}
	if req.MsgType != MsgTypeMarkdown {
		t.Error("wrong msg_type")
	}
}

func TestMessageReq_MediaJSON(t *testing.T) {
	req := buildMessageReq(MsgTypeMedia, "some_file_info_string", "msg-3", 3)
	if req.Content != "" {
		t.Error("media message should NOT have flat Content field")
	}
	if req.Media == nil || req.Media.FileInfo != "some_file_info_string" {
		t.Error("media file_info missing")
	}
	if req.MsgType != MsgTypeMedia {
		t.Error("wrong msg_type")
	}
}
