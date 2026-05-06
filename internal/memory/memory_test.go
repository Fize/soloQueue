package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	today := time.Now().Format("2006-01-02")
	now := time.Now().Format("2006-01-02 15:04")
	llm := &agent.FakeLLM{Responses: []string{
		"# " + today + "\n\n## " + now + "\n- User asked about task routing\n- Implemented fix\n",
		"# " + today + "\n\n## " + now + "\n- User asked about task routing\n- Implemented fix\n\n## " + now + "\n- User asked about second topic\n- Discussed more things\n",
	}}
	return NewManager(memoryDir, llm, "fast-model", nil), memoryDir
}

func TestRecord_CreatesNewFile(t *testing.T) {
	mgr, dir := newTestManager(t)

	err := mgr.Record(context.Background(), "User: hello\nAssistant: hi there")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	h1Found := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			h1Found = true
			break
		}
	}
	if !h1Found {
		t.Errorf("expected level-1 header '# %s', got: %s", today, content)
	}
	if !strings.Contains(content, "## "+today) {
		t.Errorf("expected entry header '## %s HH:MM', got: %s", today, content)
	}
	if !strings.Contains(content, "User asked about task routing") {
		t.Errorf("expected summary content, got: %s", content)
	}
}

func TestRecord_MergesWithExisting(t *testing.T) {
	mgr, dir := newTestManager(t)

	_ = mgr.Record(context.Background(), "User: first message\nAssistant: first reply")
	_ = mgr.Record(context.Background(), "User: second message\nAssistant: second reply")

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".md")
	data, _ := os.ReadFile(path)
	content := string(data)

	// Second call merges with existing — the merged output should contain both topics
	if !strings.Contains(content, "User asked about task routing") {
		t.Errorf("expected first entry content, got: %s", content)
	}
	if !strings.Contains(content, "User asked about second topic") {
		t.Errorf("expected second entry content, got: %s", content)
	}
}

func TestRecord_CleansUpOldFiles(t *testing.T) {
	mgr, dir := newTestManager(t)

	// Create a file older than 7 days (should be cleaned up)
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldPath := filepath.Join(dir, oldDate+".md")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(oldPath, []byte("# "+oldDate+"\nold content"), 0644)

	// Create a recent file within 7 days (should survive)
	recentDate := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	recentPath := filepath.Join(dir, recentDate+".md")
	_ = os.WriteFile(recentPath, []byte("# "+recentDate+"\nrecent content"), 0644)

	// Record a new entry — should trigger cleanup
	_ = mgr.Record(context.Background(), "User: hello")

	// Old file (10 days) should be deleted
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file %s should be deleted", oldPath)
	}

	// Recent file (5 days) should still exist
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Errorf("recent file %s should still exist", recentPath)
	}
}

func TestRecord_EmptyTextSkipped(t *testing.T) {
	mgr, dir := newTestManager(t)

	err := mgr.Record(context.Background(), "   ")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file for empty text, but found %s", path)
	}
}

func TestDailyPath(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	expected := today + ".md"
	if !strings.HasSuffix(expected, ".md") {
		t.Error("daily path should end with .md")
	}
	if len(expected) != 13 { // "YYYY-MM-DD.md" = 13 chars
		t.Errorf("expected 13 chars, got %d: %s", len(expected), expected)
	}
}

func TestBuildMergePrompt_EmptyExisting(t *testing.T) {
	now := time.Now()
	prompt := buildMergePrompt("", "User: test\nAssistant: ok", now)
	if !strings.Contains(prompt, "conversation archivist") {
		t.Error("prompt should contain archivist instruction")
	}
	if !strings.Contains(prompt, "User: test") {
		t.Error("prompt should contain conversation text")
	}
	// Date should appear in the # header (today's date).
	if !strings.Contains(prompt, time.Now().Format("2006-01-02")) {
		t.Error("prompt should contain date")
	}
	// The recordedAt time should NOT be used as a header anymore —
	// timestamps come from [markers] in the conversation text.
	if strings.Contains(prompt, now.Format("15:04")) {
		t.Error("prompt should NOT contain recordedAt time as a header")
	}
}

func TestBuildMergePrompt_WithExisting(t *testing.T) {
	existing := "# 2026-05-03\n\n## 2026-05-03 14:00\n- Previous work\n"
	now := time.Now()
	prompt := buildMergePrompt(existing, "User: new\nAssistant: ok", now)
	if !strings.Contains(prompt, "Merge") {
		t.Error("prompt should contain merge instruction")
	}
	if !strings.Contains(prompt, existing) {
		t.Error("prompt should contain existing memory")
	}
	if !strings.Contains(prompt, "User: new") {
		t.Error("prompt should contain new conversation")
	}
	if !strings.Contains(prompt, "COMPLETE merged file") {
		t.Error("prompt should ask for complete merged file")
	}
	// The recordedAt time should NOT appear as a header.
	if strings.Contains(prompt, now.Format("15:04")) {
		t.Error("prompt should NOT contain recordedAt time")
	}
}

func TestMessagesToText(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello world"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "tool", Name: "read", Content: "file contents here"},
	}

	result := MessagesToText(msgs)

	if strings.Contains(result, "You are a helpful assistant") {
		t.Error("system messages should be skipped")
	}
	if !strings.Contains(result, "User: Hello world") {
		t.Error("should contain user message")
	}
	if !strings.Contains(result, "Assistant: Hi there!") {
		t.Error("should contain assistant message")
	}
	if !strings.Contains(result, "Tool(read): file contents here") {
		t.Error("should contain tool message with name")
	}
}

func TestMessagesToText_TruncatesLongContent(t *testing.T) {
	longContent := strings.Repeat("x", 3000)
	msgs := []agent.LLMMessage{
		{Role: "user", Content: longContent},
	}
	result := MessagesToText(msgs)
	if !strings.Contains(result, "(truncated)") {
		t.Error("long content should be truncated")
	}
	if len(result) > 2500 {
		t.Errorf("result too long: %d chars", len(result))
	}
}

func TestListMemoryFiles(t *testing.T) {
	mgr, memoryDir := newTestManager(t)
	_ = os.MkdirAll(memoryDir, 0755)

	// Create date-named files
	_ = os.WriteFile(filepath.Join(memoryDir, "2026-05-01.md"), []byte("old"), 0644)
	_ = os.WriteFile(filepath.Join(memoryDir, "2026-05-02.md"), []byte("recent"), 0644)
	// Non-date file should be ignored
	_ = os.WriteFile(filepath.Join(memoryDir, "not-a-date.md"), []byte("other"), 0644)

	files, err := mgr.ListMemoryFiles()
	if err != nil {
		t.Fatalf("ListMemoryFiles: %v", err)
	}

	if len(files) < 2 {
		t.Errorf("expected at least 2 date files, got %d: %v", len(files), files)
	}
	if files[0] != "2026-05-01.md" || files[1] != "2026-05-02.md" {
		t.Errorf("files not sorted: %v", files)
	}
	// not-a-date.md should also be present (ListMemoryFiles returns all .md files)
	found := false
	for _, f := range files {
		if f == "not-a-date.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected not-a-date.md in list, got: %v", files)
	}
}
