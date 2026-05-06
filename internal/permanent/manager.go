// Package permanent provides long-term permanent memory backed by embedding vectors.
// It periodically migrates expired short-term memory files (>7 days) to a vector store.
// Each memory entry is individually summarized via LLM before storage.
package permanent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/embedding"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/vectorstore"
)

const (
	defaultTopK       = 5
	defaultMinSim     = float32(0.6)
	defaultMaxAgeDays = 7
	maxSummaryLen     = 300 // max chars per summarized entry
	maxDisplayLen     = 200 // max chars when displaying entries in system prompt
)

// Summarizer is a minimal LLM interface for summarizing memory entries.
// Defined here to avoid import cycles (agent → skill → tools → permanent).
type Summarizer interface {
	Chat(ctx context.Context, request SummarizeRequest) (SummarizeResponse, error)
}

// SummarizeRequest mirrors agent.LLMRequest with only the fields needed for summarization.
type SummarizeRequest struct {
	Model       string
	Messages    []SummarizeMessage
	MaxTokens   int
	Temperature float64
}

// SummarizeMessage mirrors agent.LLMMessage.
type SummarizeMessage struct {
	Role    string
	Content string
}

// SummarizeResponse mirrors agent.LLMResponse with only the fields needed.
type SummarizeResponse struct {
	Content string
}

// Manager orchestrates long-term permanent memory.
type Manager struct {
	store      vectorstore.VectorStore
	embedder   embedding.Embedder
	llm        Summarizer
	modelID    string
	memoryDir  string
	maxAgeDays int
	topK       int
	minSim     float32
	logger     *logger.Logger
}

// NewManager creates a permanent memory manager.
// llm may be nil if summarization is not needed (e.g. in tests without LLM).
// modelID is the fast/cheap model to use for summarization during migration.
func NewManager(store vectorstore.VectorStore, embedder embedding.Embedder, llm Summarizer, modelID, memoryDir string, l *logger.Logger) *Manager {
	return &Manager{
		store:      store,
		embedder:   embedder,
		llm:        llm,
		modelID:    modelID,
		memoryDir:  memoryDir,
		maxAgeDays: defaultMaxAgeDays,
		topK:       defaultTopK,
		minSim:     defaultMinSim,
		logger:     l,
	}
}

// Migrate scans short-term memory files older than maxAgeDays, splits them into
// per-entry summaries via LLM, embeds each entry, upserts them to the vector store,
// and deletes the source files. Returns the number of migrated entries.
func (m *Manager) Migrate(ctx context.Context) (int, error) {
	expired, err := m.listExpiredFiles()
	if err != nil {
		return 0, fmt.Errorf("permanent: list expired: %w", err)
	}
	if len(expired) == 0 {
		return 0, nil
	}

	var migrated int
	for _, name := range expired {
		if err := ctx.Err(); err != nil {
			return migrated, err
		}

		path := filepath.Join(m.memoryDir, name)
		n, err := m.migrateFile(ctx, path, name)
		if err != nil {
			m.logError(ctx, "permanent: migrate file", err)
			return migrated, err
		}
		migrated += n
	}

	return migrated, nil
}

// migrationEntry is one parsed entry from a short-term memory file.
type migrationEntry struct {
	header  string    // the original ## header line (e.g. "## 2026-05-03 14:22 — 14:35")
	ts      time.Time // parsed timestamp
	content string    // the body text under this header
}

// migrateFile reads one short-term memory file, splits it into per-entry
// summaries, embeds and stores each as a separate permanent entry.
func (m *Manager) migrateFile(ctx context.Context, path, filename string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("permanent: read %s: %w", filename, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		os.Remove(path)
		return 1, nil // counted as 1 cleaned file
	}

	entries := m.splitEntries(content)
	if len(entries) == 0 {
		// No ## entries found; store the whole file as one entry.
		fileDate, _ := time.Parse("2006-01-02", strings.TrimSuffix(filename, ".md"))
		return m.upsertEntry(ctx, path, content, fileDate, filename)
	}

	var count int
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		summary := m.summarizeEntry(ctx, e)
		if summary == "" {
			continue
		}
		if _, err := m.upsertEntry(ctx, path, summary, e.ts, filename); err != nil {
			return count, err
		}
		count++
	}

	// Delete source file after successful migration of all entries.
	if count > 0 {
		if err := os.Remove(path); err != nil {
			m.logError(ctx, "permanent: remove source failed", err)
		}
	}

	return count, nil
}

// upsertEntry embeds and stores one entry, then returns (1, nil).
func (m *Manager) upsertEntry(ctx context.Context, path, content string, ts time.Time, source string) (int, error) {
	if m.llm == nil {
		return 0, fmt.Errorf("permanent: no LLM configured for summarization")
	}

	results, err := m.embedder.Embed(ctx, []string{content})
	if err != nil {
		return 0, fmt.Errorf("permanent: embed entry: %w", err)
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("permanent: embed returned no results")
	}

	entry := vectorstore.MemoryEntry{
		ID:        uuid.NewString(),
		Content:   content,
		Embedding: results[0].Embedding,
		Timestamp: ts,
		Source:    source,
	}
	if err := m.store.Upsert(ctx, entry); err != nil {
		return 0, fmt.Errorf("permanent: upsert entry: %w", err)
	}
	return 1, nil
}

// h2Regex matches level-2 markdown headers with optional time range.
var h2Regex = regexp.MustCompile(`(?m)^## (.+)$`)

// splitEntries splits a short-term memory file into per-##-header entries.
func (m *Manager) splitEntries(content string) []migrationEntry {
	matches := h2Regex.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	var entries []migrationEntry
	for i, match := range matches {
		// match[2], match[3] are the header text start/end (first submatch)
		header := content[match[2]:match[3]]
		ts := parseEntryTimestamp(header)

		// Body: from end of this header line to start of next, or EOF.
		bodyStart := match[1] // end of full match
		bodyEnd := len(content)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := strings.TrimSpace(content[bodyStart:bodyEnd])

		if body == "" {
			continue
		}

		entries = append(entries, migrationEntry{
			header:  header,
			ts:      ts,
			content: body,
		})
	}
	return entries
}

// parseEntryTimestamp extracts the first timestamp from an entry header like
// "2026-05-03 14:22 — 14:35" or "2026-05-03 14:22".
func parseEntryTimestamp(header string) time.Time {
	// Try full datetime first: "2026-05-03 14:22"
	ts, err := time.Parse("2006-01-02 15:04", header)
	if err == nil {
		return ts
	}
	// Try with range: "2026-05-03 14:22 — 14:35" → take the first part
	if idx := strings.Index(header, " — "); idx > 0 {
		ts, err := time.Parse("2006-01-02 15:04", header[:idx])
		if err == nil {
			return ts
		}
	}
	// Try "HH:MM" only (today's date)
	if len(header) == 5 && header[2] == ':' {
		ts, err := time.Parse("15:04", header)
		if err == nil {
			now := time.Now()
			return time.Date(now.Year(), now.Month(), now.Day(), ts.Hour(), ts.Minute(), 0, 0, now.Location())
		}
	}
	// Fallback: use file date. Return zero time so caller uses fileDate.
	return time.Time{}
}

// summarizeEntry uses the fast LLM to compress one entry into a concise summary.
func (m *Manager) summarizeEntry(ctx context.Context, e migrationEntry) string {
	if m.llm == nil {
		// No LLM: use raw body, truncate.
		body := e.content
		if len(body) > maxSummaryLen {
			body = body[:maxSummaryLen] + "..."
		}
		return body
	}

	tsStr := "unknown time"
	if !e.ts.IsZero() {
		tsStr = e.ts.Format("2006-01-02 15:04")
	}

	prompt := fmt.Sprintf(`Summarize this conversation entry into a single-line factual note. Keep all key details. Do NOT add commentary.

Time: %s

Entry:
%s

Summary (one line, under %d chars):`, tsStr, e.content, maxSummaryLen)

	req := SummarizeRequest{
		Model: m.modelID,
		Messages: []SummarizeMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.0,
	}
	resp, err := m.llm.Chat(ctx, req)
	if err != nil {
		// Fallback: use raw body truncated.
		body := e.content
		if len(body) > maxSummaryLen {
			body = body[:maxSummaryLen] + "..."
		}
		return body
	}

	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		body := e.content
		if len(body) > maxSummaryLen {
			body = body[:maxSummaryLen] + "..."
		}
		return body
	}
	if len(summary) > maxSummaryLen {
		summary = summary[:maxSummaryLen] + "..."
	}
	return summary
}

// QueryForPrompt retrieves relevant permanent memories formatted for injection
// into the system prompt. Returns an empty string if nothing relevant is found.
func (m *Manager) QueryForPrompt(ctx context.Context, queryText string) (string, error) {
	if queryText == "" {
		return "", nil
	}

	count, err := m.store.Count(ctx)
	if err != nil || count == 0 {
		return "", nil
	}

	results, err := m.embedder.Embed(ctx, []string{queryText})
	if err != nil {
		return "", fmt.Errorf("permanent: query embed: %w", err)
	}
	if len(results) == 0 {
		return "", nil
	}

	entries, err := m.store.Query(ctx, results[0].Embedding, m.topK, m.minSim)
	if err != nil {
		return "", fmt.Errorf("permanent: vector query: %w", err)
	}
	if len(entries) == 0 {
		return "", nil
	}

	return m.formatEntries(entries), nil
}

// listExpiredFiles returns sorted .md filenames whose date is before the cutoff.
func (m *Manager) listExpiredFiles() ([]string, error) {
	entries, err := os.ReadDir(m.memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -m.maxAgeDays)
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
		if fileDate.Before(cutoff) {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// formatEntries formats memory entries for system prompt injection.
// Each entry shows the full content, truncated to maxDisplayLen characters.
func (m *Manager) formatEntries(entries []vectorstore.MemoryEntry) string {
	var b strings.Builder
	for _, e := range entries {
		date := e.Timestamp.Format("2006-01-02")
		content := e.Content
		if len(content) > maxDisplayLen {
			content = content[:maxDisplayLen] + "..."
		}
		fmt.Fprintf(&b, "- [%s] %s\n", date, content)
	}
	return b.String()
}

// Remember embeds a single piece of content and saves it immediately to the vector store.
// The source is set to "manual" to distinguish from auto-migrated entries.
// If at is non-zero, it is used as the entry timestamp; otherwise time.Now() is used.
func (m *Manager) Remember(ctx context.Context, content string, at time.Time) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("permanent: remember: empty content")
	}
	if at.IsZero() {
		at = time.Now()
	}

	results, err := m.embedder.Embed(ctx, []string{content})
	if err != nil {
		return fmt.Errorf("permanent: remember embed: %w", err)
	}
	if len(results) == 0 {
		return fmt.Errorf("permanent: remember embed returned no results")
	}

	entry := vectorstore.MemoryEntry{
		ID:        uuid.NewString(),
		Content:   content,
		Embedding: results[0].Embedding,
		Timestamp: at,
		Source:    "manual",
	}
	return m.store.Upsert(ctx, entry)
}

func (m *Manager) logError(ctx context.Context, msg string, err error) {
	if m.logger != nil {
		m.logger.LogError(ctx, logger.CatApp, msg, err)
	}
}

// ─── LLM adapter ──────────────────────────────────────────────────────────

// SummarizeFunc is a function-based Summarizer for use when wrapping an existing
// LLM client without importing the agent package.
type SummarizeFunc func(ctx context.Context, req SummarizeRequest) (SummarizeResponse, error)

// Chat implements Summarizer.
func (f SummarizeFunc) Chat(ctx context.Context, req SummarizeRequest) (SummarizeResponse, error) {
	return f(ctx, req)
}
