package vectorstore

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if s := CosineSimilarity(a, b); s != 1.0 {
		t.Errorf("identical vectors: expected 1.0, got %f", s)
	}

	c := []float32{0, 1, 0}
	if s := CosineSimilarity(a, c); math.Abs(float64(s)) > 0.001 {
		t.Errorf("orthogonal vectors: expected ~0, got %f", s)
	}

	d := []float32{-1, 0, 0}
	if s := CosineSimilarity(a, d); s != -1.0 {
		t.Errorf("opposite vectors: expected -1.0, got %f", s)
	}

	// different lengths
	if s := CosineSimilarity(a, []float32{1}); s != 0 {
		t.Errorf("different lengths: expected 0, got %f", s)
	}

	// zero vector
	if s := CosineSimilarity([]float32{0, 0}, []float32{1, 2}); s != 0 {
		t.Errorf("zero vector: expected 0, got %f", s)
	}
}

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.jsonl")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store
}

func TestFileStore_Upsert_NewEntry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry := MemoryEntry{
		ID:        "1",
		Content:   "test content",
		Embedding: []float32{1, 2, 3},
	}
	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	count, _ := store.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}
}

func TestFileStore_Upsert_UpdateExisting(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "old", Embedding: []float32{1, 0}})
	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "new", Embedding: []float32{0, 1}})

	count, _ := store.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry after update, got %d", count)
	}

	// Verify content updated
	results, _ := store.Query(ctx, []float32{0, 1}, 1, 0.5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "new" {
		t.Errorf("expected 'new', got %q", results[0].Content)
	}
}

func TestFileStore_Query_ExactMatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	vec := []float32{0.5, 0.5, 0.5, 0.5}
	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "target", Embedding: vec})
	_ = store.Upsert(ctx, MemoryEntry{ID: "2", Content: "other", Embedding: []float32{-1, -1, -1, -1}})

	results, err := store.Query(ctx, vec, 5, 0.9)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 result for exact match")
	}
	if results[0].ID != "1" {
		t.Errorf("expected ID '1', got %q", results[0].ID)
	}
}

func TestFileStore_Query_MinSimilarity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "close", Embedding: []float32{1, 0}})

	// Query with an orthogonal vector — should not match
	results, _ := store.Query(ctx, []float32{0, 1}, 5, 0.5)
	if len(results) != 0 {
		t.Errorf("expected 0 results with high threshold, got %d", len(results))
	}
}

func TestFileStore_Query_TopK(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		v := float32(i+1) / 10.0
		_ = store.Upsert(ctx, MemoryEntry{
			ID:        string(rune('a' + i)),
			Content:   "entry",
			Embedding: []float32{v, 0, 0, 0},
		})
	}

	// Query with a vector closest to entry 'j' (1.0)
	results, _ := store.Query(ctx, []float32{1, 0, 0, 0}, 3, 0)
	if len(results) != 3 {
		t.Errorf("expected exactly 3 results, got %d", len(results))
	}
}

func TestFileStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.jsonl")
	ctx := context.Background()

	// Create, write, and let it go out of scope
	store1, _ := NewFileStore(path)
	_ = store1.Upsert(ctx, MemoryEntry{ID: "1", Content: "persist", Embedding: []float32{1, 2}})

	// Re-open from the same file
	store2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (reopen): %v", err)
	}

	count, _ := store2.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry after reopen, got %d", count)
	}

	results, _ := store2.Query(ctx, []float32{1, 2}, 1, 0.9)
	if len(results) != 1 || results[0].Content != "persist" {
		t.Error("persisted entry not found or corrupted")
	}
}

func TestFileStore_EmptyQuery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	results, err := store.Query(ctx, []float32{1, 0}, 5, 0)
	if err != nil {
		t.Fatalf("Query on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on empty store, got %d", len(results))
	}
}

func TestFileStore_Count(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	count, _ := store.Count(ctx)
	if count != 0 {
		t.Errorf("initial count: expected 0, got %d", count)
	}

	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "a", Embedding: []float32{1}})
	_ = store.Upsert(ctx, MemoryEntry{ID: "2", Content: "b", Embedding: []float32{2}})

	count, _ = store.Count(ctx)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestFileStore_NewFileStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "subdir")
	path := filepath.Join(dir, "entries.jsonl")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("new file should be empty, got %d bytes", info.Size())
	}
}
