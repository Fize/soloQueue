package timeline

import (
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── Writer ──────────────────────────────────────────────────────────────────

// Writer is an append-only JSONL timeline writer.
type Writer struct {
	rw     *rotating.DateSizeWriter
	logger *logger.Logger
}

// WriterOption is an optional configuration for Writer.
type WriterOption func(*Writer)

// WithWriterLogger sets the logger instance for the timeline writer.
func WithWriterLogger(l *logger.Logger) WriterOption {
	return func(w *Writer) { w.logger = l }
}

// NewWriter creates a timeline writer.
// dir is the directory where the files will be stored, baseName is the file name prefix (e.g., "timeline").
// maxBytes is the maximum size of a single file (0=unlimited), maxDays is the number of days to retain files (0=unlimited).
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

// AppendMessage appends a message event.
func (w *Writer) AppendMessage(msg *MessagePayload) error {
	evt := newEvent(EventMessage)
	msg.Timestamp = evt.Timestamp
	evt.Message = msg
	return w.writeEvent(evt)
}

// AppendControl appends a control event.
func (w *Writer) AppendControl(ctrl *ControlPayload) error {
	evt := newEvent(EventControl)
	evt.Control = ctrl
	return w.writeEvent(evt)
}

// Close closes the writer.
func (w *Writer) Close() error {
	if w.logger != nil {
		w.logger.Debug(logger.CatMessages, "timeline: writer closed")
	}
	return w.rw.Close()
}

// writeEvent serializes and writes an event.
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