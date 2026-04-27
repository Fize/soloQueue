package timeline

import (
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── Writer ──────────────────────────────────────────────────────────────────

// Writer 是 append-only JSONL 时间线写入器
//
// 内部组合 rotating.Writer 处理文件轮转，自身负责 JSON 编码。
type Writer struct {
	rw *rotating.Writer
}

// NewWriter 创建时间线写入器
//
// dir 为文件所在目录，baseName 为文件名前缀（如 "timeline"）。
// maxBytes 为单文件最大字节数（0=不限），maxFiles 为保留的轮转文件数（0=不限）。
func NewWriter(dir, baseName string, maxBytes int64, maxFiles int) (*Writer, error) {
	rw, err := rotating.Open(dir, baseName, maxBytes, maxFiles)
	if err != nil {
		return nil, fmt.Errorf("timeline: open rotating writer: %w", err)
	}
	return &Writer{rw: rw}, nil
}

// AppendMessage 追加消息事件
func (w *Writer) AppendMessage(msg *MessagePayload) error {
	evt := newEvent(EventMessage)
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
	return w.rw.Close()
}

// writeEvent 序列化并写入事件
func (w *Writer) writeEvent(evt Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("timeline: marshal event: %w", err)
	}
	_, err = w.rw.Write(data)
	return err
}
