package vectorstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileStore is a JSONL file-based VectorStore implementation.
// All entries are loaded into memory on construction.
type FileStore struct {
	path    string
	mu      sync.RWMutex
	entries []MemoryEntry
}

// NewFileStore creates a FileStore backed by the given file path.
// The file is created if it doesn't exist.
func NewFileStore(path string) (*FileStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	fs := &FileStore{path: path}
	entries, err := fs.load()
	if err != nil {
		return nil, err
	}
	fs.entries = entries
	// Create the file if it doesn't exist yet (e.g., first run).
	if entries == nil {
		if err := fs.writeUnlocked(); err != nil {
			return nil, err
		}
	}
	return fs, nil
}

// load reads and parses the JSONL file.
func (fs *FileStore) load() ([]MemoryEntry, error) {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []MemoryEntry
	for len(data) > 0 {
		// Find the next newline
		idx := -1
		for i, b := range data {
			if b == '\n' {
				idx = i
				break
			}
		}
		var line []byte
		if idx >= 0 {
			line = data[:idx]
			data = data[idx+1:]
		} else {
			line = data
			data = nil
		}
		if len(line) == 0 {
			continue
		}
		var entry MemoryEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip corrupted lines
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Upsert inserts or updates a memory entry and persists to disk.
func (fs *FileStore) Upsert(ctx context.Context, entry MemoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	found := false
	for i := range fs.entries {
		if fs.entries[i].ID == entry.ID {
			fs.entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		fs.entries = append(fs.entries, entry)
	}

	return fs.writeUnlocked()
}

// Query returns the top-K entries most similar to the query embedding.
func (fs *FileStore) Query(ctx context.Context, embedding []float32, topK int, minSimilarity float32) ([]MemoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	type scored struct {
		entry MemoryEntry
		score float32
	}

	var results []scored
	for _, e := range fs.entries {
		sim := CosineSimilarity(embedding, e.Embedding)
		if sim >= minSimilarity {
			results = append(results, scored{entry: e, score: sim})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > len(results) {
		topK = len(results)
	}

	out := make([]MemoryEntry, topK)
	for i := 0; i < topK; i++ {
		out[i] = results[i].entry
	}
	return out, nil
}

// Count returns the number of entries in the store.
func (fs *FileStore) Count(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.entries), nil
}

// writeUnlocked writes all entries to the JSONL file atomically.
// Caller must hold fs.mu (write lock).
func (fs *FileStore) writeUnlocked() error {
	tmp := fs.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, e := range fs.entries {
		if err := enc.Encode(e); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, fs.path)
}
