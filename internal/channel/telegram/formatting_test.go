package telegram

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"strings"
	"testing"
)

func TestFormatTextConvertsCommonMarkdown(t *testing.T) {
	got := FormatText(testfixtures.Chinese("formatting_test_001"))
	if !strings.Contains(got, testfixtures.Chinese("formatting_test_002")) || !strings.Contains(got, "<a href=") || !strings.Contains(got, testfixtures.Chinese("formatting_test_003")) {
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
	got := FormatText(testfixtures.Chinese("formatting_test_004"))
	if strings.Contains(got, "|---|") || strings.Contains(got, testfixtures.Chinese("formatting_test_005")) {
		t.Fatalf("FormatText() kept Markdown table syntax: %q", got)
	}
	if !strings.Contains(got, testfixtures.Chinese("formatting_test_006")) || !strings.Contains(got, testfixtures.Chinese("formatting_test_007")) {
		t.Fatalf("FormatText() = %q", got)
	}
}

func TestSplitTextUsesUnicodeCharacters(t *testing.T) {
	parts := SplitText(testfixtures.Chinese("formatting_test_008"), 3)
	if len(parts) != 2 || parts[0] != testfixtures.Chinese("formatting_test_009") || parts[1] != testfixtures.Chinese("formatting_test_010") {
		t.Fatalf("parts = %#v", parts)
	}
}
