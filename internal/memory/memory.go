// Package memory provides a short-term memory system that creates concise,
// index-like summaries of conversation segments. It triggers on context window
// compaction (summary) and /clear commands, saving daily cumulative files to
// ~/.soloqueue/memory/{date}.md.
//
// File format:
//
//	# 2026-05-03
//
//	## 2026-05-03 14:22
//	- User asked about task routing
//	- Implemented hybrid sticky level logic
//
//	## 2026-05-03 16:45
//	- Discussed memory system design
//
// Files older than 3 days are removed on each write.
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

const (
	maxAgeDays = 7
)

// Manager writes short-term memory summaries to daily markdown files.
type Manager struct {
	workDir        string
	llm            agent.LLMClient
	modelID        string
	logger         *logger.Logger
	mu             sync.Mutex
	lastRecordedAt time.Time // latest message timestamp included in any Record call; for dedup
}

// NewManager creates a new memory manager.
// workDir is typically ~/.soloqueue/memory/.
func NewManager(workDir string, llm agent.LLMClient, modelID string, l *logger.Logger) *Manager {
	return &Manager{
		workDir: workDir,
		llm:     llm,
		modelID: modelID,
		logger:  l,
	}
}

// LastRecordedAt returns the cursor of the latest message timestamp recorded.
func (m *Manager) LastRecordedAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRecordedAt
}

// AdvanceLastRecordedAt advances the cursor to t if t is later than current.
func (m *Manager) AdvanceLastRecordedAt(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.After(m.lastRecordedAt) {
		m.lastRecordedAt = t
	}
}

// Record summarizes the given conversation text, merges it with the existing
// daily memory file via the LLM, and writes the consolidated result.
// Safe for concurrent use. Uses time.Now() for file date.
func (m *Manager) Record(ctx context.Context, conversationText string) error {
	return m.RecordAt(ctx, conversationText, time.Now())
}

// RecordAt is like Record but uses recordedAt for the file date instead of
// time.Now(). This lets callers route conversation segments from different
// days into their correct date-named files.
func (m *Manager) RecordAt(ctx context.Context, conversationText string, recordedAt time.Time) error {
	conversationText = strings.TrimSpace(conversationText)
	if conversationText == "" {
		return nil
	}

	fileDate := m.fileDate(recordedAt)

	// Read existing memory for this date so the LLM can merge.
	existing, _ := m.readFile(fileDate)

	merged, err := m.mergeAndSummarize(ctx, existing, conversationText, recordedAt)
	if err != nil {
		m.logger.LogError(ctx, logger.CatApp, "memory merge failed", err)
		return nil // non-blocking
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writeFile(fileDate, merged); err != nil {
		m.logger.LogError(ctx, logger.CatApp, "memory write failed", err)
		return nil
	}

	m.cleanupOldFiles()
	return nil
}

// fileDate returns the storage file date for a given timestamp.
// Entries older than maxAgeDays are stored in today's file.
func (m *Manager) fileDate(t time.Time) string {
	entryDate := t.Format("2006-01-02")
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	if t.Before(cutoff) {
		return time.Now().Format("2006-01-02")
	}
	return entryDate
}

// readFile reads the content of a date-named memory file.
// Returns empty string if the file doesn't exist.
func (m *Manager) readFile(fileDate string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.workDir, fileDate+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// mergeAndSummarize calls the fast model to merge new conversation content with
// existing daily memory, producing a consolidated index-like summary.
func (m *Manager) mergeAndSummarize(ctx context.Context, existing, conversationText string, recordedAt time.Time) (string, error) {
	prompt := buildMergePrompt(existing, conversationText, recordedAt)
	req := agent.LLMRequest{
		Model:       m.modelID,
		Messages:    []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:   2048,
		Temperature: 0.0,
	}
	resp, err := m.llm.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("memory merge: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// writeFile writes the merged content to a date-named file atomically.
func (m *Manager) writeFile(fileDate, content string) error {
	path := filepath.Join(m.workDir, fileDate+".md")
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		return fmt.Errorf("memory mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("memory write: %w", err)
	}
	return os.Rename(tmp, path)
}

// cleanupOldFiles removes memory files older than maxAgeDays.
func (m *Manager) cleanupOldFiles() {
	entries, err := os.ReadDir(m.workDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		fileDate, parseErr := time.Parse("2006-01-02", name)
		if parseErr != nil {
			continue // not a date-named file
		}
		if fileDate.Before(cutoff) {
			path := filepath.Join(m.workDir, e.Name())
			if err := os.Remove(path); err != nil {
				m.logger.LogError(context.Background(), logger.CatApp, "memory cleanup failed", err)
			}
		}
	}
}

// buildMergePrompt creates a prompt that asks the fast model to merge new
// conversation content into the existing daily memory file. The conversationText
// already contains [YYYY-MM-DD HH:MM] timestamp markers at each turn boundary;
// the LLM should use these markers (NOT the recordedAt time) to create ## entries.
func buildMergePrompt(existing, conversationText string, recordedAt time.Time) string {
	_ = recordedAt // only used for awareness, not as a header timestamp
	date := time.Now().Format("2006-01-02")

	if existing == "" {
		return fmt.Sprintf(`You are a conversation archivist. Create a concise, index-like summary for today's memory file.

Format:
# %s

## 2026-01-15 14:22 — 14:35
- bullet points summarizing what happened

The conversation text below has [YYYY-MM-DD HH:MM] markers at turn boundaries. Use the earliest marker in a task as the ## header time. If a task spans multiple turns, add the last marker's time as a range (e.g. "14:22 — 14:35").

Grouping rules:
- Multiple turns about the SAME task or topic → merge into ONE entry (even across different timestamps)
- A DIFFERENT task or topic → create a separate entry
- A clear break or new subject after a long gap → new entry

Focus on:
- What the user asked or wanted
- What was accomplished or decided
- Key files or code that were modified
- Important outcomes or decisions

Be brief. Use bullet points.

Conversation:
%s`, date, conversationText)
	}

	return fmt.Sprintf(`You are a conversation archivist. Merge the new conversation segment into today's existing memory file.

Existing memory:
%s

New conversation segment:
%s

Instructions:
- The conversation text has [YYYY-MM-DD HH:MM] markers at turn boundaries
- Merge multiple turns about the SAME task/topic into ONE entry (even if timestamps differ)
- Only create separate ## entries for genuinely different tasks or topics
- Use the earliest marker's time as the ## header; if the task spans multiple turns, add the last marker's time as a range (e.g. "14:22 — 14:35")
- If the new segment continues a topic already in the existing file, merge into that existing entry
- Preserve other existing ## entries unchanged
- Keep it concise and index-like — under 300 words total
- Use bullet points

Output the COMPLETE merged file content (including the # DATE header and all ## entries).`, existing, conversationText)
}

// MessagesToText converts ctxwin payload messages to a plain-text format suitable
// for summarization. Skips system messages.
func MessagesToText(payloads []agent.LLMMessage) string {
	var b strings.Builder
	for _, m := range payloads {
		switch m.Role {
		case "system":
			continue
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		case "tool":
			b.WriteString("Tool(" + m.Name + "): ")
		default:
			b.WriteString(m.Role + ": ")
		}
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "...(truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// ReadRecentMemory reads all memory files from the last maxDays days and returns
// a concatenated string. Returns empty string if no memory files exist.
func (m *Manager) ReadRecentMemory(maxDays int) (string, error) {
	entries, err := os.ReadDir(m.workDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	cutoff := time.Now().AddDate(0, 0, -maxDays)
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		fileDate, parseErr := time.Parse("2006-01-02", name)
		if parseErr != nil {
			continue
		}
		if fileDate.After(cutoff) || fileDate.Equal(cutoff) {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(m.workDir, f))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// ListMemoryFiles returns all memory files sorted by name (oldest first).
func (m *Manager) ListMemoryFiles() ([]string, error) {
	entries, err := os.ReadDir(m.workDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}
