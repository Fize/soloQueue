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

// Loader[T] 是可复用的泛型配置加载器
//
// 加载优先级（后者覆盖前者）：
//
//	defaults（硬编码） → paths[0]（主配置） → paths[1]（local 覆盖） → ...
type Loader[T any] struct {
	paths    []string
	defaults T

	current T
	mu      sync.RWMutex

	log *logger.Logger
}

// NewLoader 创建 Loader[T]，传入默认值和按优先级排列的文件路径（低→高）
//
// 校验：
//   - 至少一条 path
//   - 任一 path 不得为空字符串
//   - paths 不得重复（按原始字符串比较，未展开）
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

// Load 从文件加载配置，按优先级合并
func (l *Loader[T]) Load() error {
	return l.LoadContext(context.Background())
}

// LoadContext 带 context 的 Load：在每次文件 I/O 前检查 ctx.Err()
// 注意：不会真正中断 syscall；仅在文件之间的 gap 响应取消
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
			continue // 文件不存在：跳过（不是错误）
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

// ReadFromDisk 从文件系统重新读取并合并配置，不修改 Loader 内部状态。
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

// Get 返回当前配置快照的副本（并发安全）
func (l *Loader[T]) Get() T {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Set 修改配置并持久化到 paths[0]（原子写）
func (l *Loader[T]) Set(fn func(*T)) error {
	l.mu.Lock()
	old := l.current
	updated := l.current
	fn(&updated)
	l.current = updated
	l.mu.Unlock()

	pp, err := l.primaryPath()
	if err != nil {
		l.mu.Lock()
		l.current = old
		l.mu.Unlock()
		if l.log != nil {
			l.log.WarnContext(context.Background(), logger.CatConfig, "config set: primary path resolution failed",
				"err", err.Error(),
			)
		}
		return err
	}
	if err := l.saveTo(pp, updated); err != nil {
		// 回滚
		l.mu.Lock()
		l.current = old
		l.mu.Unlock()
		if l.log != nil {
			l.log.LogError(context.Background(), logger.CatConfig, "config set failed, rolled back", err)
		}
		return err
	}

	if l.log != nil {
		l.log.InfoContext(context.Background(), logger.CatConfig, "config set successfully",
			"primary_path", pp,
		)
	}

	return nil
}

// Save 将当前 current 原子写入 paths[0]
func (l *Loader[T]) Save() error {
	return l.SaveContext(context.Background())
}

// SaveContext 带 context 的 Save
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

// saveTo 原子写：先写 .tmp，再 rename
func (l *Loader[T]) saveTo(path string, value T) error {
	if path == "" {
		return fmt.Errorf("no primary path configured")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdirall %s: %w", filepath.Dir(path), err)
	}

	data, err := toml.Marshal(value)
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

// expandPath 展开 ~ 为用户主目录
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
