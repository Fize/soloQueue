package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Loader manages ~/.soloqueue/mcp.json lifecycle.
type Loader struct {
	path     string
	mu       sync.RWMutex
	current  Config
	watcher  *fsnotify.Watcher
	onChange []func(Config)
	debMu    sync.Mutex
	debTimer *time.Timer
	log      *logger.Logger
}

// NewLoader creates a Loader for the given path.
func NewLoader(path string, log *logger.Logger) (*Loader, error) {
	path, err := expandPath(path)
	if err != nil {
		return nil, fmt.Errorf("expand path: %w", err)
	}
	return &Loader{
		path: path,
		log:  log,
	}, nil
}

// Path returns the resolved config file path.
func (l *Loader) Path() string { return l.path }

// Load reads and parses mcp.json. Creates default if file does not exist.
func (l *Loader) Load() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			l.current = DefaultConfig()
			return l.save()
		}
		return fmt.Errorf("read mcp.json: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse mcp.json: %w", err)
	}
	if cfg.Servers == nil {
		cfg.Servers = []ServerConfig{}
	}
	l.current = cfg
	return nil
}

// ReadFromDisk reads and parses mcp.json directly from disk without modifying the loader cache.
func (l *Loader) ReadFromDisk() (Config, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("read mcp.json: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse mcp.json: %w", err)
	}
	if cfg.Servers == nil {
		cfg.Servers = []ServerConfig{}
	}
	return cfg, nil
}

// Get returns a thread-safe snapshot of the current config.
func (l *Loader) Get() Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Set atomically applies a mutation and writes to disk.
func (l *Loader) Set(fn func(*Config)) error {
	l.mu.Lock()
	old := l.current
	fn(&l.current)
	if err := l.save(); err != nil {
		l.current = old
		return err
	}
	l.mu.Unlock()

	l.fireOnChange(old)
	return nil
}

// Watch starts fsnotify on the config file's parent directory.
func (l *Loader) Watch() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	l.watcher = w

	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.Close()
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return fmt.Errorf("watch dir %s: %w", dir, err)
	}

	go l.watchLoop()
	if l.log != nil {
		l.log.Debug(logger.CatMCP, "mcp config watch started", "path", l.path)
	}
	return nil
}

// OnChange registers a callback invoked after config changes.
func (l *Loader) OnChange(fn func(Config)) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onChange = append(l.onChange, fn)
	idx := len(l.onChange) - 1
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.onChange = append(l.onChange[:idx], l.onChange[idx+1:]...)
	}
}

// Close stops the file watcher and cancels pending debounce.
func (l *Loader) Close() error {
	l.debMu.Lock()
	if l.debTimer != nil {
		l.debTimer.Stop()
		l.debTimer = nil
	}
	l.debMu.Unlock()

	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}

func (l *Loader) save() error {
	if l.path == "" {
		return fmt.Errorf("no path configured")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(l.current, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (l *Loader) watchLoop() {
	defer func() {
		if r := recover(); r != nil {
			if l.log != nil {
				l.log.Error(logger.CatMCP, "watchLoop panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}
	}()

	for {
		select {
		case evt, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			if evt.Name != l.path {
				continue
			}
			if evt.Has(fsnotify.Write) || evt.Has(fsnotify.Create) || evt.Has(fsnotify.Rename) {
				l.scheduleReload()
			}
		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			if l.log != nil {
				l.log.Error(logger.CatMCP, "watcher error", "err", err.Error())
			}
		}
	}
}

func (l *Loader) scheduleReload() {
	l.debMu.Lock()
	defer l.debMu.Unlock()

	if l.debTimer != nil {
		l.debTimer.Stop()
	}

	if l.log != nil {
		l.log.Debug(logger.CatMCP, "mcp config reload scheduled", "debounce_ms", 200)
	}

	l.debTimer = time.AfterFunc(200*time.Millisecond, func() {
		if l.log != nil {
			l.log.Debug(logger.CatMCP, "mcp config hot-reload triggered after debounce")
		}
		old := l.Get()
		if err := l.Load(); err != nil {
			if l.log != nil {
				l.log.Error(logger.CatMCP, "mcp config hot-reload failed", "err", err.Error())
			}
		} else {
			if l.log != nil {
				l.log.Info(logger.CatMCP, "mcp config hot-reload completed successfully")
			}
			l.fireOnChange(old)
		}
	})
}

func (l *Loader) fireOnChange(old Config) {
	l.mu.RLock()
	cbs := make([]func(Config), len(l.onChange))
	copy(cbs, l.onChange)
	l.mu.RUnlock()

	for _, fn := range cbs {
		fn(old)
	}
}

func expandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}
