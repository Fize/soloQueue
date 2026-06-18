package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Loader[T] is a reusable generic configuration loader.
//
// Load priority (latter overrides former):
//	defaults (hardcoded) → paths[0] (primary config) → paths[1] (local override) → ...
type Loader[T any] struct {
	paths    []string
	defaults T

	current T
	mu      sync.RWMutex

	log *logger.Logger
}

// NewLoader creates a Loader[T] with defaults and file paths sorted by priority (low → high)
//
// Validation:
//   - At least one path is required.
//   - No path string can be empty.
//   - Paths must not contain duplicates (compared by raw string before expansion).
func NewLoader[T any](defaults T, paths ...string) (*Loader[T], error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("NewLoader: at least one path required")
	}
	seen := map[string]bool{}
	for i, p := range paths {
		if p == "" {
			return nil, fmt.Errorf("NewLoader: path[%d] is empty", i)
		}
		if seen[p] {
			return nil, fmt.Errorf("NewLoader: duplicate path %q", p)
		}
		seen[p] = true
	}

	l := &Loader[T]{
		paths:    paths,
		defaults: defaults,
		current:  defaults,
	}
	return l, nil
}

// Load loads configurations from files and merges them by priority.
func (l *Loader[T]) Load() error {
	return l.LoadContext(context.Background())
}

// LoadContext runs Load with context, checking ctx.Err() before each file I/O.
// Note: It does not interrupt active system calls, but only handles cancellations between files.
func (l *Loader[T]) LoadContext(ctx context.Context) error {
	start := time.Now()
	result := l.defaults
	successCount := 0

	if l.log != nil {
		l.log.DebugContext(ctx, logger.CatConfig, "config load started",
			"num_paths", len(l.paths),
		)
	}

	for _, path := range l.paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		expanded, err := expandPath(path)
		if err != nil {
			return fmt.Errorf("expand path %s: %w", path, err)
		}

		data, err := os.ReadFile(expanded)
		if os.IsNotExist(err) {
			if l.log != nil {
				l.log.DebugContext(ctx, logger.CatConfig, "config file not found, skipping",
					"path", expanded,
				)
			}
			continue // File does not exist: skip (not an error)
		}
		if err != nil {
			if l.log != nil {
				l.log.WarnContext(ctx, logger.CatConfig, "config file read failed",
					"path", expanded,
					"err", err.Error(),
				)
			}
			return fmt.Errorf("read %s: %w", expanded, err)
		}

		result, err = MergeTOML(result, data)
		if err != nil {
			if l.log != nil {
				l.log.WarnContext(ctx, logger.CatConfig, "config merge failed",
					"path", expanded,
					"err", err.Error(),
				)
			}
			return fmt.Errorf("merge %s: %w", expanded, err)
		}
		successCount++
		if l.log != nil {
			l.log.DebugContext(ctx, logger.CatConfig, "config file merged successfully",
				"path", expanded,
			)
		}
	}

	l.mu.Lock()
	l.current = result
	l.mu.Unlock()

	// Log completion
	if l.log != nil {
		duration := time.Since(start).Milliseconds()
		l.log.InfoContext(ctx, logger.CatConfig, "config load completed",
			"files_merged", successCount,
			"duration_ms", duration,
		)
	}

	return nil
}

// ReadFromDisk reads and merges configurations from the filesystem without modifying the internal state of the Loader.
func (l *Loader[T]) ReadFromDisk() (T, error) {
	var zero T
	result := l.defaults
	for _, path := range l.paths {
		expanded, err := expandPath(path)
		if err != nil {
			return zero, fmt.Errorf("expand path %s: %w", path, err)
		}

		data, err := os.ReadFile(expanded)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return zero, fmt.Errorf("read %s: %w", expanded, err)
		}

		result, err = MergeTOML(result, data)
		if err != nil {
			return zero, fmt.Errorf("merge %s: %w", expanded, err)
		}
	}
	return result, nil
}

// Get returns a copy of the current configuration snapshot (concurrency-safe).
func (l *Loader[T]) Get() T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Save atomically writes the current settings into paths[0].
func (l *Loader[T]) Save() error {
	return l.SaveContext(context.Background())
}

// SaveContext saves the current settings using a context.
func (l *Loader[T]) SaveContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.RLock()
	current := l.current
	l.mu.RUnlock()
	pp, err := l.primaryPath()
	if err != nil {
		return err
	}
	return l.saveTo(pp, current)
}

// SetLogger sets the logger for configuration tracking and debugging
func (l *Loader[T]) SetLogger(log *logger.Logger) {
	l.log = log
}

// ─── Internal ─────────────────────────────────────────────────────────────────

func (l *Loader[T]) primaryPath() (string, error) {
	if len(l.paths) == 0 {
		return "", nil
	}
	expanded, err := expandPath(l.paths[0])
	if err != nil {
		return "", fmt.Errorf("expand primary path %s: %w", l.paths[0], err)
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

	var data []byte
	var err error
	if s, ok := any(value).(Settings); ok {
		data, err = s.MarshalTOMLWithComments()
	} else {
		data, err = toml.Marshal(value)
	}
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
