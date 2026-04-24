package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// rotateWriter 是按天或按大小轮转的文件写入器
//
// byDate=true  → system/team 层：文件名 "{prefix}-2025-04-21.jsonl"，每天零点切换，启动时清理 maxDays 天前文件
// byDate=false → session 层：文件名 "{prefix}.jsonl"，超过 maxSize 追加 .1 .2 ...
type rotateWriter struct {
	dir     string
	prefix  string
	byDate  bool
	maxSize int64 // bytes，仅 byDate=false 时有效
	maxDays int   // 仅 byDate=true 时有效

	current *os.File
	curSize int64
	curDate string // "2025-04-21"

	mu sync.Mutex
}

// newRotateWriter 创建轮转写入器并打开初始文件
func newRotateWriter(dir, prefix string, byDate bool, maxSizeMB, maxDays int) (*rotateWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}

	rw := &rotateWriter{
		dir:     dir,
		prefix:  prefix,
		byDate:  byDate,
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		maxDays: maxDays,
	}

	if err := rw.open(); err != nil {
		return nil, err
	}

	// 启动时清理过期文件（仅 byDate=true）
	if byDate {
		rw.cleanup()
	}

	return rw, nil
}

// Write 写入一行 JSON（追加 \n）
func (rw *rotateWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if err := rw.rotate(); err != nil {
		return 0, err
	}

	n, err := rw.current.Write(p)
	if err != nil {
		return n, err
	}
	// 追加换行
	if len(p) == 0 || p[len(p)-1] != '\n' {
		_, _ = rw.current.Write([]byte("\n"))
	}
	rw.curSize += int64(n) + 1
	return n, nil
}

// Close 关闭当前文件
func (rw *rotateWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.current != nil {
		return rw.current.Close()
	}
	return nil
}

// open 打开或追加到当前目标文件
func (rw *rotateWriter) open() error {
	path := rw.currentPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}

	if rw.current != nil {
		_ = rw.current.Close()
	}
	rw.current = f
	rw.curSize = info.Size()
	rw.curDate = today()
	return nil
}

// rotate 检查是否需要切换文件
func (rw *rotateWriter) rotate() error {
	if rw.byDate {
		// 按天切换
		if today() != rw.curDate {
			return rw.open()
		}
	} else {
		// 按大小切换
		if rw.maxSize > 0 && rw.curSize >= rw.maxSize {
			return rw.rollSize()
		}
	}
	return nil
}

// rollSize 按大小滚动：将当前文件依次重命名 .jsonl → .1 → .2 ...
func (rw *rotateWriter) rollSize() error {
	if rw.current != nil {
		_ = rw.current.Close()
		rw.current = nil
	}

	base := filepath.Join(rw.dir, rw.prefix+".jsonl")

	// 找最大编号
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
	// 从高到低依次重命名
	for i := maxN; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", base, i), fmt.Sprintf("%s.%d", base, i+1))
	}
	_ = os.Rename(base, base+".1")

	return rw.open()
}

// currentPath 返回当前应写入的文件路径
func (rw *rotateWriter) currentPath() string {
	if rw.byDate {
		return filepath.Join(rw.dir, fmt.Sprintf("%s-%s.jsonl", rw.prefix, today()))
	}
	return filepath.Join(rw.dir, rw.prefix+".jsonl")
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
