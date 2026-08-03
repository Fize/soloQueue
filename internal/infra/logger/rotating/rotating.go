// Package rotating provides a size-based rotating file writer
//
// When the file exceeds maxSize, perform rename chain rotation:
//
//	{base}.jsonl → {base}.1.jsonl, {base}.1.jsonl → {base}.2.jsonl, ...
//
// When the number of rotated files exceeds maxFiles, delete the oldest file.
// Used by both logger and timeline packages.
package rotating

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
)

// ─── Writer ──────────────────────────────────────────────────────────────────

// Writer is a size-based rotating file writer
type Writer struct {
	mu       sync.Mutex
	dir      string
	baseName string // e.g. "timeline" (without extension)
	current  *os.File
	curSize  int64
	maxSize  int64 // Max bytes per file, 0=unlimited
	maxFiles int   // Number of rotated files to keep (excluding active), 0=unlimited
	closed   bool
}

// Open creates or appends to a rotating file
//
// dir is the file directory (auto-created), baseName is the file prefix (e.g. "timeline").
// maxSize is max bytes per file (0=unlimited), maxFiles is number of rotated files to keep (0=unlimited).
// Naming: active file {baseName}.jsonl, rotated files {baseName}.jsonl.1, {baseName}.jsonl.2, ...
func Open(dir, baseName string, maxSize int64, maxFiles int) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", dir, err)
	}

	w := &Writer{
		dir:      dir,
		baseName: baseName,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}

	if err := w.open(); err != nil {
		return nil, err
	}

	return w, nil
}

// Write writes data (auto-appends \n), triggers rotation when limit exceeded
// After Close, writes are silently discarded.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, nil
	}

	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}

	n, err := w.current.Write(p)
	if err != nil {
		return n, err
	}
	// Append newline
	if len(p) == 0 || p[len(p)-1] != '\n' {
		if _, err := w.current.Write([]byte("\n")); err != nil {
			return n, err
		}
		w.curSize += int64(n) + 1
	} else {
		w.curSize += int64(n)
	}

	return n, nil
}

// Close closes the current file
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.current != nil {
		return w.current.Close()
	}
	return nil
}

// CurrentPath returns the current active file path
func (w *Writer) CurrentPath() string {
	return w.activePath()
}

// SetMaxSize dynamically sets max bytes per file
//
// Mainly for testing. Production code should specify at Open.
func (w *Writer) SetMaxSize(maxSize int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxSize = maxSize
}

// ─── Internal methods ─────────────────────────────────────────────────────────

// open opens or appends to the current active file
func (w *Writer) open() error {
	path := w.activePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open file %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}

	if w.current != nil {
		_ = w.current.Close()
	}
	w.current = f
	w.curSize = info.Size()
	return nil
}

// rotateIfNeeded checks if rotation is needed
func (w *Writer) rotateIfNeeded() error {
	if w.maxSize > 0 && w.curSize >= w.maxSize {
		return w.rollSize()
	}
	return nil
}

// rollSize rolls by size: rename current file sequentially, then open new file
func (w *Writer) rollSize() error {
	if w.current != nil {
		if err := w.current.Close(); err != nil {
			return fmt.Errorf("close current file: %w", err)
		}
		w.current = nil
	}

	base := w.basePath()

	// Find max number
	maxN := 0
	for {
		p := fmt.Sprintf("%s.%d", base, maxN+1)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		maxN++
		if maxN > 999 {
			break
		}
	}

	// Rename from high to low
	for i := maxN; i >= 1; i-- {
		if err := os.Rename(fmt.Sprintf("%s.%d", base, i), fmt.Sprintf("%s.%d", base, i+1)); err != nil {
			return fmt.Errorf("rotate rename %s.%d: %w", base, i, err)
		}
	}
	if err := os.Rename(base, base+".1"); err != nil {
		return fmt.Errorf("rotate rename %s: %w", base, err)
	}

	// Delete old files exceeding maxFiles
	w.trimFiles()

	return w.open()
}

// trimFiles deletes rotated files exceeding maxFiles
func (w *Writer) trimFiles() {
	if w.maxFiles <= 0 {
		return
	}
	base := w.basePath()
	for n := w.maxFiles + 1; n <= 999; n++ {
		p := fmt.Sprintf("%s.%d", base, n)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		_ = os.Remove(p)
	}
}

// activePath returns the current active file path
func (w *Writer) activePath() string {
	return filepath.Join(w.dir, w.baseName+".jsonl")
}

// basePath returns the base path without number (for rotation renaming)
func (w *Writer) basePath() string {
	return filepath.Join(w.dir, w.baseName+".jsonl")
}

// ─── File listing ─────────────────────────────────────────────────────────────

// ListFiles returns all rotated file paths (oldest to newest)
//
// Scans dir for files matching {baseName}.jsonl, {baseName}.jsonl.1, ...,
// sorted by number (oldest → newest).
func ListFiles(dir, baseName string) ([]string, error) {
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(baseName) + `\.jsonl(?:\.(\d+))?$`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type fileEntry struct {
		path string
		num  int // 0 = active file (no number), 1/2/... = rotated files
	}
	var files []fileEntry

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		num := 0
		if m[1] != "" {
			num, _ = strconv.Atoi(m[1])
		}
		files = append(files, fileEntry{
			path: filepath.Join(dir, e.Name()),
			num:  num,
		})
	}

	// Sort by number: smaller numbers are older (written first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].num < files[j].num
	})

	// For rotated files, higher numbers are actually older (.5 written before .1),
	// but .0 (active file) is newest. Need to reconsider sorting:
	// Actual time order: .5 (oldest) → .4 → .3 → .2 → .1 → .0 (newest)
	// So higher numbers read first
	sort.Slice(files, func(i, j int) bool {
		// Number 0 (active) comes last
		if files[i].num == 0 {
			return false
		}
		if files[j].num == 0 {
			return true
		}
		// Higher numbers are older, come first
		return files[i].num > files[j].num
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result, nil
}
