package rotating

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

const dateLayout = "2006-01-02"

type DateSizeOption func(*DateSizeWriter)

// WithDateSizeMaxFiles limits historical files, excluding the current file; zero is unlimited.
func WithDateSizeMaxFiles(n int) DateSizeOption { return func(w *DateSizeWriter) { w.maxFiles = n } }

func WithDateSizeNow(now func() time.Time) DateSizeOption {
	return func(w *DateSizeWriter) {
		if now != nil {
			w.now = now
		}
	}
}

// DateSizeWriter rotates JSONL files by date and size.
type DateSizeWriter struct {
	mu       sync.Mutex
	dir      string
	baseName string
	current  *os.File
	curDate  string
	curSeq   int
	curSize  int64
	maxSize  int64
	maxDays  int
	maxFiles int
	closed   bool
	now      func() time.Time
}

// OpenDateSize creates a writer that rotates files by day and by size.
// Files are named {baseName}-YYYY-MM-DD.jsonl, {baseName}-YYYY-MM-DD-2.jsonl, ...
func OpenDateSize(dir, baseName string, maxSize int64, maxDays int, opts ...DateSizeOption) (*DateSizeWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", dir, err)
	}

	w := &DateSizeWriter{
		dir:      dir,
		baseName: baseName,
		maxSize:  maxSize,
		maxDays:  maxDays,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(w)
	}

	w.refreshPolicy()
	if err := w.openLatestForDate(w.today()); err != nil {
		return nil, err
	}
	w.cleanupLocked()
	return w, nil
}

func (w *DateSizeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, nil
	}

	w.refreshPolicy()
	date := w.today()
	if date != w.curDate {
		if err := w.openLatestForDate(date); err != nil {
			return 0, err
		}
		w.cleanupLocked()
	}

	writeLen := int64(len(p))
	if len(p) == 0 || p[len(p)-1] != '\n' {
		writeLen++
	}
	if w.maxSize > 0 && w.curSize > 0 && w.curSize+writeLen > w.maxSize {
		if err := w.openSeq(w.curDate, w.curSeq+1); err != nil {
			return 0, err
		}
		w.cleanupLocked()
	}

	n, err := w.current.Write(p)
	if err != nil {
		return n, err
	}
	w.curSize += int64(n)
	if len(p) == 0 || p[len(p)-1] != '\n' {
		if _, err := w.current.Write([]byte("\n")); err != nil {
			return n, err
		}
		w.curSize++
	}
	return n, nil
}

func (w *DateSizeWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.current != nil {
		markActive(w.pathFor(w.curDate, w.curSeq), -1)
		return w.current.Close()
	}
	return nil
}

func (w *DateSizeWriter) CurrentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.curDate == "" {
		return ""
	}
	return w.pathFor(w.curDate, w.curSeq)
}

// SetMaxSize dynamically sets max bytes per file.
// Mainly for testing. Production code should specify the limit at OpenDateSize.
func (w *DateSizeWriter) SetMaxSize(maxSize int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxSize = maxSize
}

func (w *DateSizeWriter) today() string {
	return w.now().Format(dateLayout)
}

func (w *DateSizeWriter) openLatestForDate(date string) error {
	seq, err := latestDateSizeSeq(w.dir, w.baseName, date)
	if err != nil {
		return err
	}
	if seq == 0 {
		seq = 1
	}
	if err := w.openSeq(date, seq); err != nil {
		return err
	}
	if w.maxSize > 0 && w.curSize >= w.maxSize {
		return w.openSeq(date, seq+1)
	}
	return nil
}

func (w *DateSizeWriter) openSeq(date string, seq int) error {
	if seq <= 0 {
		seq = 1
	}
	path := w.pathFor(date, seq)
	markActive(path, 1)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		markActive(path, -1)
		return fmt.Errorf("open file %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		markActive(path, -1)
		return err
	}
	if w.current != nil {
		markActive(w.pathFor(w.curDate, w.curSeq), -1)
		_ = w.current.Close()
	}
	w.current = f
	w.curDate = date
	w.curSeq = seq
	w.curSize = info.Size()
	return nil
}

func (w *DateSizeWriter) pathFor(date string, seq int) string {
	return filepath.Join(w.dir, dateSizeFileName(w.baseName, date, seq))
}

func (w *DateSizeWriter) refreshPolicy() {
	if p, ok := resolvePolicy(w.dir, w.baseName); ok {
		w.maxSize = p.MaxBytes
		w.maxDays = p.MaxDays
		w.maxFiles = p.MaxFiles
	}
}
func (w *DateSizeWriter) cleanupLocked() {
	files, err := ListDateSizeFiles(w.dir, w.baseName)
	if err != nil {
		return
	}
	cutoff := startOfDay(w.now()).AddDate(0, 0, 1-w.maxDays)
	var historical []string
	for _, path := range files {
		if IsActive(path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		m := dateSizePattern(w.baseName).FindStringSubmatch(filepath.Base(path))
		d, err := time.ParseInLocation(dateLayout, m[1], w.now().Location())
		if err != nil {
			continue
		}
		if w.maxDays > 0 && d.Before(cutoff) {
			_ = RemoveInactive(path, func() error { return os.Remove(path) })
			continue
		}
		historical = append(historical, path)
	}
	if w.maxFiles > 0 {
		for len(historical) > w.maxFiles {
			_ = RemoveInactive(historical[0], func() error { return os.Remove(historical[0]) })
			historical = historical[1:]
		}
	}
}

func dateSizeFileName(baseName, date string, seq int) string {
	if seq <= 1 {
		return fmt.Sprintf("%s-%s.jsonl", baseName, date)
	}
	return fmt.Sprintf("%s-%s-%d.jsonl", baseName, date, seq)
}

func dateSizePattern(baseName string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(baseName) + `-(\d{4}-\d{2}-\d{2})(?:-(\d+))?\.jsonl$`)
}

func latestDateSizeSeq(dir, baseName, date string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	re := dateSizePattern(baseName)
	maxSeq := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(e.Name())
		if m == nil || m[1] != date {
			continue
		}
		seq := 1
		if m[2] != "" {
			seq, _ = strconv.Atoi(m[2])
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return maxSeq, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// ListDateSizeFiles returns date-size rotated files in chronological order.
func ListDateSizeFiles(dir, baseName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type fileEntry struct {
		path string
		date string
		seq  int
	}
	var files []fileEntry
	re := dateSizePattern(baseName)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		seq := 1
		if m[2] != "" {
			seq, _ = strconv.Atoi(m[2])
		}
		files = append(files, fileEntry{
			path: filepath.Join(dir, e.Name()),
			date: m[1],
			seq:  seq,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].date != files[j].date {
			return files[i].date < files[j].date
		}
		return files[i].seq < files[j].seq
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result, nil
}
