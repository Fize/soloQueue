package permanent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/embedding"
	"github.com/xiaobaitu/soloqueue/internal/vectorstore"
)

// fixedEmbedder returns the same vector for every input.
type fixedEmbedder struct {
	dim   int
	vec   []float32
	err   error
	calls int
}

func (f *fixedEmbedder) Embed(ctx context.Context, texts []string) ([]embedding.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	results := make([]embedding.Result, len(texts))
	for i := range texts {
		results[i] = embedding.Result{Embedding: f.vec, Tokens: len(texts[i])}
	}
	return results, nil
}

func (f *fixedEmbedder) Dimension() int { return f.dim }

func newTestManager(t *testing.T, store vectorstore.VectorStore, embedder embedding.Embedder, memoryDir string) *Manager {
	t.Helper()
	return NewManager(store, embedder, memoryDir, nil)
}

func newTestStore(t *testing.T) (vectorstore.VectorStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := vectorstore.NewFileStore(filepath.Join(dir, "entries.jsonl"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store, dir
}

func TestMigrate_EmptyDir(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: []float32{1, 0, 0, 0}}
	mgr := newTestManager(t, store, emb, memoryDir)

	count, err := mgr.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 migrated, got %d", count)
	}
}

func TestMigrate_MigratesExpiredFiles(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: []float32{1, 0, 0, 0}}
	mgr := newTestManager(t, store, emb, memoryDir)

	// Create a file older than 7 days
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldPath := filepath.Join(memoryDir, oldDate+".md")
	_ = os.WriteFile(oldPath, []byte("# "+oldDate+"\n\n## "+oldDate+" 12:00\n- Test content"), 0644)

	// Create a recent file (should stay)
	recentDate := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	recentPath := filepath.Join(memoryDir, recentDate+".md")
	_ = os.WriteFile(recentPath, []byte("# "+recentDate+"\nrecent"), 0644)

	count, err := mgr.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 migrated, got %d", count)
	}

	// Old file should be deleted
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("expired file should be deleted")
	}
	// Recent file should remain
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Error("recent file should still exist")
	}

	// Store should have the entry
	ctx := context.Background()
	storeCount, _ := store.Count(ctx)
	if storeCount != 1 {
		t.Errorf("expected 1 entry in store, got %d", storeCount)
	}
}

func TestMigrate_EmbedFails(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: nil, err: errors.New("embed failed")}
	mgr := newTestManager(t, store, emb, memoryDir)

	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldPath := filepath.Join(memoryDir, oldDate+".md")
	_ = os.WriteFile(oldPath, []byte("content"), 0644)

	_, err := mgr.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected error from embed failure")
	}

	// File should NOT be deleted
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Error("file should remain after failed embed")
	}
}

func TestMigrate_SkipsEmptyFile(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: []float32{1, 0, 0, 0}}
	mgr := newTestManager(t, store, emb, memoryDir)

	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldPath := filepath.Join(memoryDir, oldDate+".md")
	_ = os.WriteFile(oldPath, []byte("   "), 0644)

	count, err := mgr.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 (empty file deleted), got %d", count)
	}
	// Empty file should be deleted
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("empty expired file should be deleted")
	}
}

func TestQueryForPrompt_NoResults(t *testing.T) {
	store, storeDir := newTestStore(t)
	_ = storeDir
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: []float32{1, 0, 0, 0}}
	mgr := newTestManager(t, store, emb, memoryDir)

	result, err := mgr.QueryForPrompt(context.Background(), "some query")
	if err != nil {
		t.Fatalf("QueryForPrompt: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestQueryForPrompt_FindsResults(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	vec := []float32{1, 0, 0, 0}
	emb := &fixedEmbedder{dim: 4, vec: vec}
	mgr := newTestManager(t, store, emb, memoryDir)

	// Pre-populate the store
	_ = store.Upsert(context.Background(), vectorstore.MemoryEntry{
		ID:        "1",
		Content:   "User prefers functional programming",
		Embedding: vec,
		Timestamp: time.Now(),
		Source:    "2026-04-01.md",
	})

	result, err := mgr.QueryForPrompt(context.Background(), "programming style")
	if err != nil {
		t.Fatalf("QueryForPrompt: %v", err)
	}
	if !strings.Contains(result, "functional programming") {
		t.Errorf("expected result to contain 'functional programming', got: %s", result)
	}
}

func TestListExpiredFiles(t *testing.T) {
	store, _ := newTestStore(t)
	memoryDir := t.TempDir()
	emb := &fixedEmbedder{dim: 4, vec: []float32{1, 0, 0, 0}}
	mgr := newTestManager(t, store, emb, memoryDir)

	create := func(daysAgo int, name string) {
		date := time.Now().AddDate(0, 0, -daysAgo).Format("2006-01-02")
		_ = os.WriteFile(filepath.Join(memoryDir, date+".md"), []byte(name), 0644)
	}

	create(10, "old")
	create(3, "recent")
	create(8, "also-old")
	// Non-date file — should be ignored
	_ = os.WriteFile(filepath.Join(memoryDir, "not-a-date.md"), []byte("x"), 0644)

	files, err := mgr.listExpiredFiles()
	if err != nil {
		t.Fatalf("listExpiredFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 expired files, got %d: %v", len(files), files)
	}
}
