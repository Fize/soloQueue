package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Loader[T] is a reusable generic configuration loader.
//
// Load priority: defaults (hardcoded) → primary config file.
type Loader[T any] struct {
	path     string
	defaults T

	current T
	mu      sync.RWMutex

	watcher   *fsnotify.Watcher
	watchMu   sync.Mutex
	watchStop chan struct{}
	watchPath string
	onChange  func() error

	lastWrite time.Time
	writeMu   sync.Mutex
}

// NewLoader creates a Loader[T] with defaults and a single config file path.
func NewLoader[T any](defaults T, path string) (*Loader[T], error) {
	if path == "" {
		return nil, errors.New("NewLoader: path required")
	}

	l := &Loader[T]{
		path:     path,
		defaults: defaults,
		current:  defaults,
	}
	return l, nil
}

// Load loads configurations from files and merges them by priority.
func (l *Loader[T]) Load() error {
	return l.LoadContext(context.Background())
}

// LoadContext loads the single config file over the defaults.
func (l *Loader[T]) LoadContext(ctx context.Context) error {
	result := l.defaults
	if err := ctx.Err(); err != nil {
		return err
	}

	expanded, err := l.expandedPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			// No file yet: keep defaults.
			l.mu.Lock()
			l.current = result
			l.mu.Unlock()
			return nil
		}
		return fmt.Errorf("read %s: %w", expanded, err)
	}

	if err := yaml.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parse %s: %w", expanded, err)
	}

	l.mu.Lock()
	l.current = result
	l.mu.Unlock()
	return nil
}

// ReadFromDisk reads and merges configurations from the filesystem without modifying the internal state of the Loader.
func (l *Loader[T]) ReadFromDisk() (T, error) {
	var zero T
	result := l.defaults
	expanded, err := l.expandedPath()
	if err != nil {
		return zero, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return zero, fmt.Errorf("read %s: %w", expanded, err)
	}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("parse %s: %w", expanded, err)
	}
	return result, nil
}

// Get returns a copy of the current configuration snapshot (concurrency-safe).
func (l *Loader[T]) Get() T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Set applies a mutation to the current config, persists it to disk, and returns the updated snapshot.
func (l *Loader[T]) Set(mutate func(*T)) (T, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	mutate(&l.current)

	l.writeMu.Lock()
	l.lastWrite = time.Now()
	l.writeMu.Unlock()

	pp, err := l.expandedPath()
	if err != nil {
		return l.current, err
	}
	return l.current, l.saveTo(pp, l.current)
}

// Save atomically writes the current settings to the config file.
func (l *Loader[T]) Save() error {
	return l.SaveContext(context.Background())
}

// SaveContext saves the current settings using a context.
func (l *Loader[T]) SaveContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	current := l.current
	l.mu.Unlock()
	pp, err := l.expandedPath()
	if err != nil {
		return err
	}
	return l.saveTo(pp, current)
}

// SetOnChange registers a callback invoked after a file change is detected by Watch.
func (l *Loader[T]) SetOnChange(fn func() error) {
	l.mu.Lock()
	l.onChange = fn
	l.mu.Unlock()
}

// Watch starts watching the config file for external changes and reloads automatically.
// It calls onChange (if set) after reloading.
func (l *Loader[T]) Watch() error {
	l.watchMu.Lock()
	defer l.watchMu.Unlock()
	if l.watcher != nil {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}

	pp, err := l.expandedPath()
	if err != nil {
		watcher.Close()
		return err
	}
	dir := filepath.Dir(pp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		watcher.Close()
		return err
	}
	// Watch the directory so we also detect file deletion/recreation.
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("watch %s: %w", dir, err)
	}

	l.watcher = watcher
	l.watchPath = pp
	l.watchStop = make(chan struct{})

	go l.watchLoop()
	return nil
}

// StopWatch stops the fsnotify watcher.
func (l *Loader[T]) StopWatch() {
	l.watchMu.Lock()
	defer l.watchMu.Unlock()
	if l.watcher == nil {
		return
	}
	close(l.watchStop)
	l.watcher.Close()
	l.watcher = nil
}

func (l *Loader[T]) watchLoop() {
	for {
		select {
		case <-l.watchStop:
			return
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			if event.Name != l.watchPath {
				continue
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Rename) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Remove) {
				continue
			}
			// Ignore events caused by our own Save for a short window.
			l.writeMu.Lock()
			skip := time.Since(l.lastWrite) < 100*time.Millisecond
			l.writeMu.Unlock()
			if skip {
				continue
			}
			if err := l.Load(); err != nil {
				// Errors are swallowed; a partial/broken file may be transient.
				_ = err
			}
			l.mu.RLock()
			onChange := l.onChange
			l.mu.RUnlock()
			if onChange != nil {
				_ = onChange()
			}
		case _, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (l *Loader[T]) expandedPath() (string, error) {
	expanded, err := expandPath(l.path)
	if err != nil {
		return "", fmt.Errorf("expand path %s: %w", l.path, err)
	}
	return expanded, nil
}

// saveTo performs an atomic write: writes to .tmp first, then renames.
func (l *Loader[T]) saveTo(path string, value T) error {
	if path == "" {
		return fmt.Errorf("no primary path configured")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdirall %s: %w", filepath.Dir(path), err)
	}

	data, err := marshalYAML(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}

	return nil
}

// expandPath expands ~ into the user's home directory.
func expandPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}

func marshalYAML(v interface{}) ([]byte, error) {
	if s, ok := v.(Settings); ok {
		return s.MarshalYAMLWithComments()
	}
	return yaml.Marshal(v)
}
