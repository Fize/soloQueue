package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// rotateWriter 是统一按天和按大小轮转的文件写入器。
type rotateWriter struct {
	writer *rotating.DateSizeWriter
}

// newRotateWriter 创建轮转写入器并打开初始文件。
// byDate 和 maxFiles 参数保留用于兼容旧调用；当前所有日志统一按天+按大小轮转。
func newRotateWriter(dir, prefix string, _ bool, maxSizeMB, maxDays, _ int) (*rotateWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}
	maxBytes := int64(maxSizeMB) * 1024 * 1024
	w, err := rotating.OpenDateSize(dir, prefix, maxBytes, maxDays)
	if err != nil {
		return nil, err
	}
	return &rotateWriter{writer: w}, nil
}

// Write 写入一行 JSON（追加 \n）。
// After Close, writes are silently discarded.
func (rw *rotateWriter) Write(p []byte) (int, error) {
	return rw.writer.Write(p)
}

// Close 关闭当前文件。
func (rw *rotateWriter) Close() error {
	return rw.writer.Close()
}

func today() string {
	return time.Now().Format("2006-01-02")
}
