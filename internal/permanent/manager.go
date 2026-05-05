// Package permanent provides long-term permanent memory backed by embedding vectors.
// It periodically migrates expired short-term memory files (>7 days) to a vector store.
package permanent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
)

// Manager orchestrates long-term permanent memory.
type Manager struct {
	store      vectorstore.VectorStore
	embedder   embedding.Embedder
	memoryDir  string
	maxAgeDays int
	topK       int
	minSim     float32
	logger     *logger.Logger
}

// NewManager creates a permanent memory manager.
func NewManager(store vectorstore.VectorStore, embedder embedding.Embedder, memoryDir string, l *logger.Logger) *Manager {
	return &Manager{
		store:      store,
		embedder:   embedder,
		memoryDir:  memoryDir,
		maxAgeDays: defaultMaxAgeDays,
		topK:       defaultTopK,
		minSim:     defaultMinSim,
		logger:     l,
	}
}

// Migrate scans short-term memory files older than maxAgeDays, embeds their
// content, upserts them into the vector store, and deletes the source files
// on success. Returns the number of migrated files.
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
		data, err := os.ReadFile(path)
		if err != nil {
			m.logError(ctx, "permanent: read file", err)
			return migrated, fmt.Errorf("permanent: read %s: %w", name, err)
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			os.Remove(path)
			migrated++
			continue
		}

		results, err := m.embedder.Embed(ctx, []string{content})
		if err != nil {
			m.logError(ctx, "permanent: embed failed", err)
			return migrated, fmt.Errorf("permanent: embed %s: %w", name, err)
		}
		if len(results) == 0 {
			return migrated, fmt.Errorf("permanent: embed %s returned no results", name)
		}

		fileDate, _ := time.Parse("2006-01-02", strings.TrimSuffix(name, ".md"))

		entry := vectorstore.MemoryEntry{
			ID:        uuid.NewString(),
			Content:   content,
			Embedding: results[0].Embedding,
			Timestamp: fileDate,
			Source:    name,
		}
		if err := m.store.Upsert(ctx, entry); err != nil {
			m.logError(ctx, "permanent: upsert failed", err)
			return migrated, fmt.Errorf("permanent: upsert %s: %w", name, err)
		}

		if err := os.Remove(path); err != nil {
			m.logError(ctx, "permanent: remove source failed", err)
		}
		migrated++
	}

	return migrated, nil
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
func (m *Manager) formatEntries(entries []vectorstore.MemoryEntry) string {
	var b strings.Builder
	for _, e := range entries {
		date := e.Timestamp.Format("2006-01-02")
		// Extract first line of content as summary
		summary := firstLine(e.Content)
		fmt.Fprintf(&b, "- [%s] %s\n", date, summary)
	}
	return b.String()
}

func firstLine(s string) string {
	idx := strings.IndexAny(s, "\r\n")
	if idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}

// Remember embeds a single piece of content and saves it immediately to the vector store.
// The source is set to "manual" to distinguish from auto-migrated entries.
func (m *Manager) Remember(ctx context.Context, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("permanent: remember: empty content")
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
		Timestamp: time.Now(),
		Source:    "manual",
	}
	return m.store.Upsert(ctx, entry)
}

func (m *Manager) logError(ctx context.Context, msg string, err error) {
	if m.logger != nil {
		m.logger.LogError(ctx, logger.CatApp, msg, err)
	}
}
