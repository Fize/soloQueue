package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// rotateWriter 是按天或按大小轮转的文件写入器
//
// byDate=true  → system/team 层：文件名 "{prefix}-2025-04-21.jsonl"，每天零点切换，启动时清理 maxDays 天前文件
// byDate=false → session 层：委托给 rotating.Writer 处理按大小轮转
type rotateWriter struct {
	// bySize 模式：委托给 rotating.Writer
	rw *rotating.Writer

	// byDate 模式：自行管理
	byDate   bool
	maxDays  int
	current  *os.File
	curDate  string // "2025-04-21"
	dir      string
	prefix   string

	mu sync.Mutex
}

// newRotateWriter 创建轮转写入器并打开初始文件
func newRotateWriter(dir, prefix string, byDate bool, maxSizeMB, maxDays, maxFiles int) (*rotateWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}

	rw := &rotateWriter{
		dir:     dir,
		prefix:  prefix,
		byDate:  byDate,
		maxDays: maxDays,
	}

	if byDate {
		// 按天切换模式：自行管理文件
		if err := rw.openDateFile(); err != nil {
			return nil, err
		}
		// 启动时清理过期文件
		rw.cleanup()
	} else {
		// 按大小轮转模式：委托给 rotating.Writer
		maxBytes := int64(maxSizeMB) * 1024 * 1024
		r, err := rotating.Open(dir, prefix, maxBytes, maxFiles)
		if err != nil {
			return nil, err
		}
		rw.rw = r
	}

	return rw, nil
}

// Write 写入一行 JSON（追加 \n）
func (rw *rotateWriter) Write(p []byte) (int, error) {
	if rw.byDate {
		return rw.writeByDate(p)
	}
	return rw.rw.Write(p)
}

// Close 关闭当前文件
func (rw *rotateWriter) Close() error {
	if rw.byDate {
		rw.mu.Lock()
		defer rw.mu.Unlock()
		if rw.current != nil {
			return rw.current.Close()
		}
		return nil
	}
	return rw.rw.Close()
}

// ─── byDate 模式的实现 ──────────────────────────────────────────────────────

// writeByDate 按天模式的写入
func (rw *rotateWriter) writeByDate(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	// 检查是否需要切换到新日期的文件
	if today() != rw.curDate {
		if err := rw.openDateFile(); err != nil {
			return 0, err
		}
	}

	n, err := rw.current.Write(p)
	if err != nil {
		return n, err
	}
	// 追加换行
	if len(p) == 0 || p[len(p)-1] != '\n' {
		_, _ = rw.current.Write([]byte("\n"))
	}
	return n, nil
}

// openDateFile 打开当前日期对应的日志文件
func (rw *rotateWriter) openDateFile() error {
	path := filepath.Join(rw.dir, fmt.Sprintf("%s-%s.jsonl", rw.prefix, today()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}

	if rw.current != nil {
		_ = rw.current.Close()
	}
	rw.current = f
	rw.curDate = today()
	return nil
}

// cleanup 删除超过 maxDays 天的日志文件（仅 byDate=true）
func (rw *rotateWriter) cleanup() {
	if rw.maxDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -rw.maxDays)
	entries, err := os.ReadDir(rw.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(rw.dir, e.Name()))
		}
	}
}

// today 返回当前日期字符串 "2025-04-21"
func today() string {
	return time.Now().Format("2006-01-02")
}
