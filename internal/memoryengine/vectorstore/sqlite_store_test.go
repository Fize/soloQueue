package vectorstore

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_Upsert_NewEntry(t *testing.T) {
	store := newTestSQLiteStore(t)
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

func TestSQLiteStore_Upsert_UpdateExisting(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "old", Embedding: []float32{1, 0}})
	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "new", Embedding: []float32{0, 1}})

	count, _ := store.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry after update, got %d", count)
	}

	results, _ := store.Query(ctx, []float32{0, 1}, 1, 0.5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "new" {
		t.Errorf("expected 'new', got %q", results[0].Content)
	}
}

func TestSQLiteStore_Query_ExactMatch(t *testing.T) {
	store := newTestSQLiteStore(t)
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

func TestSQLiteStore_Query_MinSimilarity(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, MemoryEntry{ID: "1", Content: "close", Embedding: []float32{1, 0}})

	results, _ := store.Query(ctx, []float32{0, 1}, 5, 0.5)
	if len(results) != 0 {
		t.Errorf("expected 0 results with high threshold, got %d", len(results))
	}
}

func TestSQLiteStore_Query_TopK(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		v := float32(i+1) / 10.0
		_ = store.Upsert(ctx, MemoryEntry{
			ID:        string(rune('a' + i)),
			Content:   "entry",
			Embedding: []float32{v, 0, 0, 0},
		})
	}

	results, _ := store.Query(ctx, []float32{1, 0, 0, 0}, 3, 0)
	if len(results) != 3 {
		t.Errorf("expected exactly 3 results, got %d", len(results))
	}
}

func TestSQLiteStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entries.db")
	ctx := context.Background()

	store1, _ := NewSQLiteStore(path)
	_ = store1.Upsert(ctx, MemoryEntry{ID: "1", Content: "persist", Embedding: []float32{1, 2}})
	store1.Close()

	store2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore (reopen): %v", err)
	}
	defer store2.Close()

	count, _ := store2.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry after reopen, got %d", count)
	}

	results, _ := store2.Query(ctx, []float32{1, 2}, 1, 0.9)
	if len(results) != 1 || results[0].Content != "persist" {
		t.Error("persisted entry not found or corrupted")
	}
}

func TestSQLiteStore_EmptyQuery(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	results, err := store.Query(ctx, []float32{1, 0}, 5, 0)
	if err != nil {
		t.Fatalf("Query on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on empty store, got %d", len(results))
	}
}

func TestSQLiteStore_Count(t *testing.T) {
	store := newTestSQLiteStore(t)
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

func TestSQLiteStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "subdir")
	path := filepath.Join(dir, "entries.db")

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
}

func TestEncodeDecodeEmbedding(t *testing.T) {
	original := []float32{1.0, -0.5, 0.25, 0.0}
	encoded := encodeEmbedding(original)
	decoded := decodeEmbedding(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("index %d: expected %f, got %f", i, original[i], decoded[i])
		}
	}
}

func TestEncodeDecodeEmptyEmbedding(t *testing.T) {
	encoded := encodeEmbedding(nil)
	if encoded != nil {
		t.Error("expected nil for nil input")
	}

	encoded = encodeEmbedding([]float32{})
	if encoded != nil {
		t.Error("expected nil for empty slice")
	}

	decoded := decodeEmbedding(nil)
	if decoded != nil {
		t.Error("expected nil for nil input")
	}

	decoded = decodeEmbedding([]byte{})
	if decoded != nil {
		t.Error("expected nil for empty input")
	}
}
