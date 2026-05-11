package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// transport handles JSON-RPC 2.0 communication over stdio pipes using
// the LSP Content-Length framing protocol.
type transport struct {
	reader  *bufio.Reader
	writer  io.Writer
	writeMu sync.Mutex
}

func newTransport(stdin io.Reader, stdout io.Writer) *transport {
	return &transport{
		reader: bufio.NewReader(stdin),
		writer: stdout,
	}
}

// readMessage reads a single LSP message from stdin.
// Format: Content-Length: <N>\r\n\r\n<JSON body>
func (t *transport) readMessage() ([]byte, error) {
	var contentLength int
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header line: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			v := strings.TrimSpace(line[len("Content-Length:"):])
			contentLength, err = strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("parse Content-Length %q: %w", v, err)
			}
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("missing or invalid Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(t.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// writeMessage writes a JSON-RPC message to stdout with Content-Length framing.
func (t *transport) writeMessage(body []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := t.writer.Write([]byte(header)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := t.writer.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// sendRequest marshals and sends a JSON-RPC request.
func (t *transport) sendRequest(id any, method string, params any) error {
	body, err := json.Marshal(Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  safeMarshal(params),
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return t.writeMessage(body)
}

// sendNotification marshals and sends a JSON-RPC notification (no id field).
func (t *transport) sendNotification(method string, params any) error {
	body, err := json.Marshal(Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  safeMarshal(params),
	})
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	return t.writeMessage(body)
}

func safeMarshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
