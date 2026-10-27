package logger

import (
	"fmt"
	"os"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger/rotating"
)

// rotateWriter is a unified file writer that rotates by date and size.
type rotateWriter struct {
	writer *rotating.DateSizeWriter
}

// newRotateWriter creates a rotating writer and opens the initial file.
// Date rotation is always enabled; maxFiles excludes the active file.
func newRotateWriter(dir, prefix string, _ bool, maxSizeMB, maxDays, maxFiles int) (*rotateWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}
	maxBytes := int64(maxSizeMB) * 1024 * 1024
	w, err := rotating.OpenDateSize(dir, prefix, maxBytes, maxDays, rotating.WithDateSizeMaxFiles(maxFiles))
	if err != nil {
		return nil, err
	}
	return &rotateWriter{writer: w}, nil
}

// Write writes a JSON line (appends \n).
// After Close, writes are silently discarded.
func (rw *rotateWriter) Write(p []byte) (int, error) {
	return rw.writer.Write(p)
}

// Close closes the current file.
func (rw *rotateWriter) Close() error {
	return rw.writer.Close()
}
