package timeline

import (
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── Writer ──────────────────────────────────────────────────────────────────

// Writer 是 append-only JSONL 时间线写入器。
type Writer struct {
	rw     *rotating.DateSizeWriter
	logger *logger.Logger
}

// WriterOption 是 Writer 的可选配置
type WriterOption func(*Writer)

// WithWriterLogger 设置时间线写入器的日志实例
func WithWriterLogger(l *logger.Logger) WriterOption {
	return func(w *Writer) { w.logger = l }
}

// NewWriter 创建时间线写入器。
// dir 为文件所在目录，baseName 为文件名前缀（如 "timeline"）。
// maxBytes 为单文件最大字节数（0=不限），maxDays 为保留天数（0=不限）。
func NewWriter(dir, baseName string, maxBytes int64, maxDays int, opts ...WriterOption) (*Writer, error) {
	rw, err := rotating.OpenDateSize(dir, baseName, maxBytes, maxDays)
	if err != nil {
		return nil, fmt.Errorf("timeline: open rotating writer: %w", err)
	}
	w := &Writer{rw: rw}
	for _, opt := range opts {
		opt(w)
	}
	return w, nil
}

// AppendMessage 追加消息事件
func (w *Writer) AppendMessage(msg *MessagePayload) error {
	evt := newEvent(EventMessage)
	msg.Timestamp = evt.Timestamp
	evt.Message = msg
	return w.writeEvent(evt)
}

// AppendControl 追加控制事件
func (w *Writer) AppendControl(ctrl *ControlPayload) error {
	evt := newEvent(EventControl)
	evt.Control = ctrl
	return w.writeEvent(evt)
}

// Close 关闭写入器
func (w *Writer) Close() error {
	if w.logger != nil {
		w.logger.Debug(logger.CatMessages, "timeline: writer closed")
	}
	return w.rw.Close()
}

// writeEvent 序列化并写入事件
func (w *Writer) writeEvent(evt Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		if w.logger != nil {
			w.logger.Warn(logger.CatMessages, "timeline: marshal failed",
				"event_type", string(evt.EventType), "err", err.Error())
		}
		return fmt.Errorf("timeline: marshal event: %w", err)
	}
	_, err = w.rw.Write(data)
	if err != nil && w.logger != nil {
		w.logger.Warn(logger.CatMessages, "timeline: write failed",
			"event_type", string(evt.EventType), "err", err.Error())
	}
	return err
}
