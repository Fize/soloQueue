package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/fsnotify/fsnotify"
)

// Loader[T] 是可复用的泛型配置加载器
//
// 加载优先级（后者覆盖前者）：
//
//	defaults（硬编码） → paths[0]（主配置） → paths[1]（local 覆盖） → ...
//
// 线程安全：
//
//	mu     保护 current 快照
//	cbMu   独立保护 onChange 回调切片
//	errMu  独立保护 errHandler
//
// 锁顺序（防死锁）：
//  1. mu.Lock() → 修改 current → mu.Unlock()
//  2. cbMu.RLock() → 复制 onChange 切片 → cbMu.RUnlock()
//  3. 在所有锁释放后逐个调用回调（回调内可安全调用 Get()）
type Loader[T any] struct {
	paths    []string
	defaults T

	current T
	mu      sync.RWMutex

	onChange []callbackEntry[T]
	nextID   atomic.Uint64
	cbMu     sync.RWMutex

	watcher  *fsnotify.Watcher
	debTimer *time.Timer
	debMu    sync.Mutex

	errHandler func(error)
	errMu      sync.RWMutex
}

// callbackEntry 关联一个 OnChange 注册的回调和它的取消 ID
type callbackEntry[T any] struct {
	id uint64
	fn func(old, new T)
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
	result := l.defaults

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
			continue // 文件不存在：跳过（不是错误）
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", expanded, err)
		}

		result, err = MergeTOML(result, data)
		if err != nil {
			return fmt.Errorf("merge %s: %w", expanded, err)
		}
	}

	// 获取旧值，更新 current
	l.mu.Lock()
	old := l.current
	l.current = result
	l.mu.Unlock()

	// 在锁外通知回调
	l.notify(old, result)
	return nil
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
		return err
	}
	if err := l.saveTo(pp, updated); err != nil {
		// 回滚
		l.mu.Lock()
		l.current = old
		l.mu.Unlock()
		return err
	}

	// 在锁外通知回调
	l.notify(old, updated)
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

// Watch 启动 fsnotify 监听所有 paths，变更时 debounce 200ms 后 Load()
func (l *Loader[T]) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}
	l.watcher = watcher

	// 监听所有存在的路径（及其父目录，兼容文件创建事件）
	watched := map[string]bool{}
	for _, path := range l.paths {
		expanded, err := expandPath(path)
		if err != nil {
			return fmt.Errorf("expand path %s: %w", path, err)
		}
		dir := filepath.Dir(expanded)
		if !watched[dir] {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create config dir %s: %w", dir, err)
			}
			if err := watcher.Add(dir); err != nil {
				return fmt.Errorf("watch config dir %s: %w", dir, err)
			}
			watched[dir] = true
		}
	}

	go l.watchLoop()
	return nil
}

// OnChange 注册配置变更回调，返回取消函数
//
// 调用取消函数后该回调不再触发；重复调用取消函数无害（幂等）
// 老 caller 忽略返回值依然可用
func (l *Loader[T]) OnChange(fn func(old, new T)) (cancel func()) {
	id := l.nextID.Add(1)
	l.cbMu.Lock()
	l.onChange = append(l.onChange, callbackEntry[T]{id: id, fn: fn})
	l.cbMu.Unlock()

	return func() {
		l.cbMu.Lock()
		defer l.cbMu.Unlock()
		for i, e := range l.onChange {
			if e.id == id {
				l.onChange = append(l.onChange[:i], l.onChange[i+1:]...)
				return
			}
		}
	}
}

// SetErrorHandler 设置 fsnotify watcher 的 error 回调
// nil 表示不处理（默认行为）
// config 包不依赖 logger 包；由 caller 决定如何记录 error
func (l *Loader[T]) SetErrorHandler(fn func(error)) {
	l.errMu.Lock()
	defer l.errMu.Unlock()
	l.errHandler = fn
}

// Close 停止 fsnotify watcher 并取消 debounce timer
func (l *Loader[T]) Close() error {
	l.debMu.Lock()
	if l.debTimer != nil {
		l.debTimer.Stop()
	}
	l.debMu.Unlock()

	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}

// ─── Internal ─────────────────────────────────────────────────────────────────

func (l *Loader[T]) watchLoop() {
	for {
		select {
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			// 仅处理受监控文件的写入/创建/重命名事件
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				if l.isWatchedFile(event.Name) {
					l.scheduleReload()
				}
			}
		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			l.handleWatchError(err)
		}
	}
}

func (l *Loader[T]) handleWatchError(err error) {
	l.errMu.RLock()
	fn := l.errHandler
	l.errMu.RUnlock()
	if fn != nil {
		fn(err)
	}
}

func (l *Loader[T]) scheduleReload() {
	l.debMu.Lock()
	defer l.debMu.Unlock()

	if l.debTimer != nil {
		l.debTimer.Stop()
	}
	l.debTimer = time.AfterFunc(200*time.Millisecond, func() {
		if err := l.Load(); err != nil {
			l.handleWatchError(fmt.Errorf("config reload: %w", err))
		}
	})
}

func (l *Loader[T]) isWatchedFile(name string) bool {
	absName, err := filepath.Abs(name)
	if err != nil {
		return false
	}
	for _, path := range l.paths {
		expanded, err := expandPath(path)
		if err != nil {
			continue
		}
		absPath, err := filepath.Abs(expanded)
		if err != nil {
			continue
		}
		if absName == absPath {
			return true
		}
	}
	return false
}

func (l *Loader[T]) notify(old, new T) {
	l.cbMu.RLock()
	entries := make([]callbackEntry[T], len(l.onChange))
	copy(entries, l.onChange)
	l.cbMu.RUnlock()

	for _, e := range entries {
		e.fn(old, new)
	}
}

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
