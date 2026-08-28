// Package dispatch persists session-owned agent work dispatches.
package dispatch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Kind string

const (
	KindDelegate Kind = "delegate"
	KindPeerHelp Kind = "peer_help"
)

type Status string

const (
	StatusRunning            Status = "running"
	StatusPersistencePending Status = "persistence_pending"
	StatusCompleted          Status = "completed"
	StatusFailed             Status = "failed"
	StatusInterrupted        Status = "interrupted"
)

var (
	ErrActiveConflict     = errors.New("active dispatch identity has different content")
	ErrPersistencePending = errors.New("dispatch terminal persistence is pending")
)

type terminalIntent struct {
	Status Status
	Error  string
}

type Record struct {
	ID                 string    `json:"dispatch_id"`
	Kind               Kind      `json:"kind"`
	TaskName           string    `json:"task_name"`
	Task               string    `json:"task"`
	Context            string    `json:"context,omitempty"`
	OwnerSessionID     string    `json:"owner_session_id"`
	Requester          string    `json:"requester"`
	Executor           string    `json:"executor"`
	ExecutorInstanceID string    `json:"executor_instance_id,omitempty"`
	RootID             string    `json:"root_dispatch_id"`
	ParentID           string    `json:"parent_dispatch_id,omitempty"`
	Status             Status    `json:"status"`
	Revision           uint64    `json:"revision"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	TaskKey            string    `json:"task_key"`
	ContentHash        string    `json:"content_hash"`
	Error              string    `json:"error,omitempty"`
}

type BeginInput struct {
	Kind      Kind
	TaskName  string
	Task      string
	Context   string
	Requester string
	Executor  string
	RootID    string
	ParentID  string
}

type BeginResult struct {
	Record Record
	Reused bool
}

type Event struct {
	Sequence  uint64          `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Record    Record          `json:"record"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	root       string
	ownerID    string
	records    map[string]Record
	pending    map[string]terminalIntent
	writeEvent func(*os.File, []byte) (int, error)
	writeMeta  func(string, any) error
}

// NewManager constructs the single dispatch manager owned by one Session.
// SoloQueue does not support multiple managers for the same session timeline.
func NewManager(timelineRoot, ownerSessionID string) (*Manager, error) {
	if strings.TrimSpace(timelineRoot) == "" || strings.TrimSpace(ownerSessionID) == "" {
		return nil, errors.New("dispatch: timeline root and owner session ID are required")
	}
	m := &Manager{
		root:       filepath.Join(timelineRoot, "delegations"),
		ownerID:    ownerSessionID,
		records:    make(map[string]Record),
		pending:    make(map[string]terminalIntent),
		writeEvent: (*os.File).Write,
		writeMeta:  writeAtomicJSON,
	}
	if err := os.MkdirAll(filepath.Join(m.root, ".active"), 0o700); err != nil {
		return nil, fmt.Errorf("dispatch: create directory: %w", err)
	}
	if err := m.reconcile(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Begin(in BeginInput) (BeginResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in.TaskName = strings.TrimSpace(in.TaskName)
	in.Task = strings.TrimSpace(in.Task)
	in.Executor = strings.TrimSpace(in.Executor)
	in.Requester = strings.TrimSpace(in.Requester)
	if in.TaskName == "" || in.Task == "" || in.Executor == "" || in.Requester == "" {
		return BeginResult{}, errors.New("dispatch: task_name, task, requester, and executor are required")
	}
	if in.Kind == "" {
		in.Kind = KindDelegate
	}
	key := logicalKey(m.ownerID, in)
	hash := digest(in.Task + "\x00" + in.Context)
	for id, intent := range m.pending {
		if rec := m.records[id]; rec.TaskKey == key {
			if err := m.retryTerminalLocked(id, intent); err != nil {
				return BeginResult{}, errors.Join(ErrPersistencePending, err)
			}
		}
	}
	claimPath := filepath.Join(m.root, ".active", key+".json")
	if claim, err := readClaim(claimPath); err == nil {
		existing, ok := m.records[claim.ID]
		if ok && existing.OwnerSessionID == m.ownerID && existing.Status == StatusRunning {
			if existing.ContentHash != hash {
				return BeginResult{}, fmt.Errorf("%w: task %q is already running as %s", ErrActiveConflict, in.TaskName, existing.ID)
			}
			return BeginResult{Record: existing, Reused: true}, nil
		}
		_ = os.Remove(claimPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return BeginResult{}, err
	}

	id, err := newID()
	if err != nil {
		return BeginResult{}, err
	}
	now := time.Now().UTC()
	rec := Record{ID: "dlg_" + id, Kind: in.Kind, TaskName: in.TaskName, Task: in.Task, Context: in.Context, OwnerSessionID: m.ownerID,
		Requester: in.Requester, Executor: in.Executor, ParentID: in.ParentID, RootID: in.RootID,
		Status: StatusRunning, Revision: 1, CreatedAt: now, UpdatedAt: now, TaskKey: key, ContentHash: hash}
	if rec.RootID == "" {
		rec.RootID = rec.ID
	}
	if err := os.MkdirAll(filepath.Join(m.root, rec.ID), 0o700); err != nil {
		return BeginResult{}, err
	}
	claimData, _ := json.Marshal(claim{ID: rec.ID})
	f, err := os.OpenFile(claimPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(filepath.Join(m.root, rec.ID))
		return BeginResult{}, fmt.Errorf("dispatch: reserve active task: %w", err)
	}
	if _, err = f.Write(claimData); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(claimPath)
		_ = os.RemoveAll(filepath.Join(m.root, rec.ID))
		return BeginResult{}, err
	}
	m.records[rec.ID] = rec
	committed, err := m.appendLocked(rec.ID, "created", nil, rec)
	if err != nil {
		if committed {
			projectionErr := fmt.Errorf("dispatch %s persistence projection: %w", rec.ID, err)
			failed := rec
			failed.Status = StatusFailed
			failed.Error = projectionErr.Error()
			failed.Revision++
			failed.UpdatedAt = time.Now().UTC()
			terminalCommitted, terminalErr := m.appendLocked(rec.ID, string(StatusFailed), nil, failed)
			if terminalCommitted {
				m.records[rec.ID] = failed
				_ = os.Remove(claimPath)
			} else {
				m.pending[rec.ID] = terminalIntent{Status: StatusFailed, Error: projectionErr.Error()}
				pendingRecord := rec
				pendingRecord.Status = StatusPersistencePending
				pendingRecord.Error = errors.Join(projectionErr, terminalErr).Error()
				m.records[rec.ID] = pendingRecord
				failed = pendingRecord
			}
			return BeginResult{Record: failed}, errors.Join(projectionErr, terminalErr)
		}
		delete(m.records, rec.ID)
		_ = os.Remove(claimPath)
		_ = os.RemoveAll(filepath.Join(m.root, rec.ID))
		return BeginResult{}, err
	}
	return BeginResult{Record: rec}, nil
}

func (m *Manager) AssignExecutorInstance(id, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return os.ErrNotExist
	}
	if intent, pending := m.pending[id]; pending {
		if err := m.retryTerminalLocked(id, intent); err != nil {
			return errors.Join(ErrPersistencePending, err)
		}
		return nil
	}
	if rec.Status != StatusRunning {
		return fmt.Errorf("dispatch: cannot assign executor to terminal dispatch %s", id)
	}
	rec.ExecutorInstanceID = instanceID
	rec.Revision++
	rec.UpdatedAt = time.Now().UTC()
	committed, err := m.appendLocked(id, "executor_assigned", map[string]string{"executor_instance_id": instanceID}, rec)
	if committed {
		m.records[id] = rec
	}
	return err
}

func (m *Manager) Append(id, eventType string, payload any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return os.ErrNotExist
	}
	if _, pending := m.pending[id]; pending {
		return ErrPersistencePending
	}
	if rec.Status != StatusRunning {
		return fmt.Errorf("dispatch: cannot append to terminal dispatch %s", id)
	}
	rec.Revision++
	rec.UpdatedAt = time.Now().UTC()
	committed, err := m.appendLocked(id, eventType, payload, rec)
	if committed {
		m.records[id] = rec
	}
	return err
}

func (m *Manager) Finish(id string, status Status, errValue error) error {
	if status != StatusCompleted && status != StatusFailed && status != StatusInterrupted {
		return fmt.Errorf("dispatch: invalid terminal status %q", status)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return os.ErrNotExist
	}
	if intent, pending := m.pending[id]; pending {
		if err := m.retryTerminalLocked(id, intent); err != nil {
			return errors.Join(ErrPersistencePending, err)
		}
		return nil
	}
	if rec.Status != StatusRunning {
		return nil
	}
	rec.Status = status
	rec.Revision++
	rec.UpdatedAt = time.Now().UTC()
	if errValue != nil {
		rec.Error = errValue.Error()
	}
	committed, persistErr := m.appendLocked(id, string(status), nil, rec)
	if !committed {
		intent := terminalIntent{Status: status}
		if errValue != nil {
			intent.Error = errValue.Error()
		}
		m.pending[id] = intent
		pendingRecord := rec
		pendingRecord.Status = StatusPersistencePending
		pendingRecord.Error = persistErr.Error()
		m.records[id] = pendingRecord
		return persistErr
	}
	m.records[id] = rec
	delete(m.pending, id)
	claimErr := os.Remove(filepath.Join(m.root, ".active", rec.TaskKey+".json"))
	return errors.Join(persistErr, claimErr)
}

func (m *Manager) Get(id string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if intent, pending := m.pending[id]; pending {
		_ = m.retryTerminalLocked(id, intent)
	}
	rec, ok := m.records[id]
	return rec, ok
}

func (m *Manager) List() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, intent := range m.pending {
		_ = m.retryTerminalLocked(id, intent)
	}
	out := make([]Record, 0, len(m.records))
	for _, rec := range m.records {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Manager) Tail(id string, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if intent, pending := m.pending[id]; pending {
		if err := m.retryTerminalLocked(id, intent); err != nil {
			return nil, errors.Join(ErrPersistencePending, err)
		}
	}
	if _, ok := m.records[id]; !ok {
		return nil, os.ErrNotExist
	}
	events, err := readEvents(filepath.Join(m.root, id))
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (m *Manager) appendLocked(id, eventType string, payload any, rec Record) (bool, error) {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		raw = data
	}
	event := Event{Sequence: rec.Revision, Timestamp: rec.UpdatedAt, Type: eventType, Record: rec, Payload: raw}
	data, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	streamPath := filepath.Join(m.root, id, "stream-"+event.Timestamp.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(streamPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	priorOffset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return false, err
	}
	line := append(data, '\n')
	n, writeErr := m.writeEvent(f, line)
	if writeErr == nil && n != len(line) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if writeErr != nil {
		truncateErr := f.Truncate(priorOffset)
		rollbackSyncErr := f.Sync()
		closeErr := f.Close()
		return false, errors.Join(writeErr, truncateErr, rollbackSyncErr, closeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		// The authoritative event was durably synced before Close reported its
		// error, so callers must treat it as committed.
		return true, closeErr
	}
	if err := m.writeMeta(filepath.Join(m.root, id, "meta.json"), rec); err != nil {
		return true, err
	}
	return true, nil
}

func (m *Manager) reconcile() error {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "dlg_") {
			continue
		}
		events, err := readEvents(filepath.Join(m.root, entry.Name()))
		if err != nil {
			return fmt.Errorf("dispatch: reconcile %s: %w", entry.Name(), err)
		}
		if len(events) == 0 {
			continue
		}
		rec := events[len(events)-1].Record
		if rec.OwnerSessionID != m.ownerID {
			continue
		}
		m.records[rec.ID] = rec
		if err := writeAtomicJSON(filepath.Join(m.root, rec.ID, "meta.json"), rec); err != nil {
			return err
		}
	}
	for id, rec := range m.records {
		if rec.Status != StatusRunning {
			continue
		}
		rec.Status = StatusInterrupted
		rec.Revision++
		rec.UpdatedAt = time.Now().UTC()
		rec.Error = "process restarted before dispatch completed"
		committed, persistErr := m.appendLocked(id, string(StatusInterrupted), nil, rec)
		if !committed {
			return persistErr
		}
		m.records[id] = rec
		_ = os.Remove(filepath.Join(m.root, ".active", rec.TaskKey+".json"))
		if persistErr != nil {
			return persistErr
		}
	}
	return nil
}

type claim struct {
	ID string `json:"dispatch_id"`
}

func (m *Manager) retryTerminalLocked(id string, intent terminalIntent) error {
	authoritative, ok := m.loadRecord(id)
	if !ok || authoritative.OwnerSessionID != m.ownerID {
		return fmt.Errorf("dispatch %s authoritative record unavailable", id)
	}
	if authoritative.Status != StatusRunning {
		m.records[id] = authoritative
		delete(m.pending, id)
		return nil
	}
	candidate := authoritative
	candidate.Status = intent.Status
	candidate.Error = intent.Error
	candidate.Revision++
	candidate.UpdatedAt = time.Now().UTC()
	committed, persistErr := m.appendLocked(id, string(intent.Status), nil, candidate)
	if !committed {
		pendingRecord := authoritative
		pendingRecord.Status = StatusPersistencePending
		pendingRecord.Error = persistErr.Error()
		m.records[id] = pendingRecord
		return persistErr
	}
	m.records[id] = candidate
	delete(m.pending, id)
	claimErr := os.Remove(filepath.Join(m.root, ".active", authoritative.TaskKey+".json"))
	return errors.Join(persistErr, claimErr)
}

func (m *Manager) loadRecord(id string) (Record, bool) {
	events, err := readEvents(filepath.Join(m.root, id))
	if err != nil || len(events) == 0 {
		return Record{}, false
	}
	return events[len(events)-1].Record, true
}

func readClaim(path string) (claim, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return claim{}, err
	}
	var c claim
	err = json.Unmarshal(data, &c)
	return c, err
}

func readEvents(dir string) ([]Event, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "stream-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var events []Event
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(f)
		for {
			var event Event
			if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("dispatch: decode %s: %w", path, err)
			}
			events = append(events, event)
		}
		_ = f.Close()
	}
	return events, nil
}

func writeAtomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".meta-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func logicalKey(owner string, in BeginInput) string {
	return digest(strings.Join([]string{owner, in.ParentID, string(in.Kind), strings.ToLower(in.Executor), strings.ToLower(strings.Join(strings.Fields(in.TaskName), " "))}, "\x00"))
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("dispatch: generate UUID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type contextKey struct{}

type Scope struct {
	Manager  *Manager
	RootID   string
	ParentID string
}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, contextKey{}, scope)
}
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(contextKey{}).(Scope)
	return s, ok && s.Manager != nil
}
