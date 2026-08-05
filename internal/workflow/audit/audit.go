// Package audit implements append-only, hash-chained workflow audit logs.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)("?\s*[:=]\s*"?)([^"\s,}\]]+)`)

type Entry struct {
	Sequence  int64           `json:"sequence"`
	RunID     string          `json:"run_id"`
	NodeRunID string          `json:"node_run_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
	PrevHash  string          `json:"prev_hash,omitempty"`
	Hash      string          `json:"hash"`
}

type Log struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	sequence int64
	head     string
}

func Open(dir, runID string) (*Log, error) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("workflow_audit_invalid: directory and run id are required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("workflow_audit_mkdir: %w", err)
	}
	path := fmt.Sprintf("%s/%s.jsonl", strings.TrimRight(dir, "/"), runID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("workflow_audit_open: %w", err)
	}
	log := &Log{file: file, path: path}
	if err := log.loadTail(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return log, nil
}

func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Log) Head() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.head
}

func (l *Log) Append(runID, nodeRunID, eventType string, payload any) (Entry, error) {
	if l == nil || l.file == nil {
		return Entry{}, fmt.Errorf("workflow_audit_closed")
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(eventType) == "" {
		return Entry{}, fmt.Errorf("workflow_audit_invalid: run id and event type are required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Entry{}, fmt.Errorf("workflow_audit_payload: %w", err)
	}
	raw = []byte(secretPattern.ReplaceAllString(string(raw), `$1$2[REDACTED]`))
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sequence++
	entry := Entry{Sequence: l.sequence, RunID: runID, NodeRunID: nodeRunID, Type: eventType, Payload: raw, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), PrevHash: l.head}
	canonical, _ := json.Marshal(entry)
	digest := sha256.Sum256(canonical)
	entry.Hash = hex.EncodeToString(digest[:])
	line, _ := json.Marshal(entry)
	if _, err := l.file.Write(append(line, '\n')); err != nil {
		l.sequence--
		return Entry{}, fmt.Errorf("workflow_audit_write: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return Entry{}, fmt.Errorf("workflow_audit_sync: %w", err)
	}
	l.head = entry.Hash
	return entry, nil
}

func (l *Log) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Log) loadTail() error {
	file, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("workflow_audit_read: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("workflow_audit_read: %w", err)
	}
	line, err := readLastNonEmptyLine(file, info.Size())
	if err != nil {
		return fmt.Errorf("workflow_audit_read: %w", err)
	}
	if len(line) == 0 {
		return nil
	}
	var last Entry
	if err := json.Unmarshal(line, &last); err != nil {
		return fmt.Errorf("workflow_audit_corrupt: %w", err)
	}
	l.sequence = last.Sequence
	l.head = last.Hash
	return nil
}

func readLastNonEmptyLine(file *os.File, size int64) ([]byte, error) {
	const chunkSize int64 = 4096
	var tail []byte
	for end := size; end > 0; {
		start := end - chunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, end-start)
		n, err := file.ReadAt(chunk, start)
		if err != nil && err != io.EOF {
			return nil, err
		}
		combined := make([]byte, n+len(tail))
		copy(combined, chunk[:n])
		copy(combined[n:], tail)
		tail = bytes.TrimSpace(combined)
		if separator := bytes.LastIndexByte(tail, '\n'); separator >= 0 {
			return bytes.TrimSpace(tail[separator+1:]), nil
		}
		end = start
	}
	return bytes.TrimSpace(tail), nil
}
