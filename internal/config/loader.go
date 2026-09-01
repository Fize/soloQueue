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

	current  T
	mu       sync.RWMutex
	reloadMu sync.Mutex

	watcher     *fsnotify.Watcher
	watchMu     sync.Mutex
	watchStop   chan struct{}
	onChange    func(T) error
	onCommitted func(T)
	onError     func(error)

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
	result, err := l.readCandidate(ctx)
	if err != nil {
		return err
	}
	l.reloadMu.Lock()
	defer l.reloadMu.Unlock()
	l.mu.Lock()
	l.current = result
	l.mu.Unlock()
	return nil
}

func (l *Loader[T]) readCandidate(ctx context.Context) (T, error) {
	var zero T
	result := l.defaults
	if err := ctx.Err(); err != nil {
		return zero, err
	}

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

// ReadFromDisk reads and merges configurations from the filesystem without modifying the internal state of the Loader.
func (l *Loader[T]) ReadFromDisk() (T, error) {
	return l.readCandidate(context.Background())
}

// Get returns a copy of the current configuration snapshot (concurrency-safe).
func (l *Loader[T]) Get() T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Set applies a mutation to the current config, persists it to disk, and returns the updated snapshot.
func (l *Loader[T]) Set(mutate func(*T)) (T, error) {
	l.reloadMu.Lock()
	l.mu.Lock()
	mutate(&l.current)
	candidate := l.current
	l.mu.Unlock()

	l.writeMu.Lock()
	l.lastWrite = time.Now()
	l.writeMu.Unlock()

	pp, err := l.expandedPath()
	if err != nil {
		l.reloadMu.Unlock()
		return candidate, err
	}
	if err := l.saveTo(pp, candidate); err != nil {
		l.reloadMu.Unlock()
		return candidate, err
	}
	l.reloadMu.Unlock()

	// Set is the programmatic equivalent of an accepted file-watch reload:
	// notify consumers only after the candidate has been durably written. Keep
	// this callback outside the loader locks so consumers may safely inspect
	// configuration or perform their own synchronization.
	l.mu.RLock()
	onCommitted := l.onCommitted
	l.mu.RUnlock()
	if onCommitted != nil {
		onCommitted(candidate)
	}
	return candidate, nil
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
func (l *Loader[T]) SetOnChange(fn func(T) error) {
	l.mu.Lock()
	l.onChange = fn
	l.mu.Unlock()
}

// SetOnCommitted registers an infallible notification invoked after an
// accepted file-watch candidate has been published to Get. Validation and any
// operation that may reject a candidate belong in SetOnChange; a committed
// notification cannot roll back the already-published snapshot.
func (l *Loader[T]) SetOnCommitted(fn func(T)) {
	l.mu.Lock()
	l.onCommitted = fn
	l.mu.Unlock()
}

// SetOnError registers a callback for asynchronous reload and watcher errors.
func (l *Loader[T]) SetOnError(fn func(error)) {
	l.mu.Lock()
	l.onError = fn
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
	l.watchStop = make(chan struct{})

	go l.watchLoop(watcher, l.watchStop, pp)
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

func (l *Loader[T]) watchLoop(watcher *fsnotify.Watcher, stop <-chan struct{}, path string) {
	for {
		select {
		case <-stop:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name != path {
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
			candidate, err := l.readCandidate(context.Background())
			if err != nil {
				l.mu.RLock()
				onError := l.onError
				l.mu.RUnlock()
				if onError != nil {
					onError(err)
				}
				continue
			}
			if err := l.applyReloadCandidate(candidate); err != nil {
				l.mu.RLock()
				onError := l.onError
				l.mu.RUnlock()
				if onError != nil {
					onError(err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			l.mu.RLock()
			onError := l.onError
			l.mu.RUnlock()
			if onError != nil {
				onError(err)
			}
		}
	}
}

// applyReloadCandidate lets the callback validate and apply the candidate
// before publishing it. Concurrent Get calls keep observing the last accepted
// snapshot for the complete callback window. reloadMu prevents overlapping
// file events from interleaving.
func (l *Loader[T]) applyReloadCandidate(candidate T) error {
	l.reloadMu.Lock()
	l.mu.RLock()
	onChange := l.onChange
	l.mu.RUnlock()
	if onChange != nil {
		if err := onChange(candidate); err != nil {
			l.reloadMu.Unlock()
			return fmt.Errorf("apply reloaded config: %w", err)
		}
	}
	l.mu.Lock()
	l.current = candidate
	onCommitted := l.onCommitted
	l.mu.Unlock()
	l.reloadMu.Unlock()
	if onCommitted != nil {
		onCommitted(candidate)
	}
	return nil
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
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
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
