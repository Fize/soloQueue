// Package observability stores optional execution details independently of replayable conversation history.
package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var diskMu sync.Mutex
var ErrActive = errors.New("execution is active")

type Policy struct{ MaxBytes, MaxTurnBytes, MaxFileBytes int64 }
type Store struct {
	root   string
	policy func() Policy
}

func ForWorkDir(dir string) *Store {
	if resolved, e := filepath.EvalSymlinks(dir); e == nil {
		dir = resolved
	}
	return &Store{root: filepath.Join(dir, "logs", "observability")}
}
func (s *Store) SetPolicy(fn func() Policy) { s.policy = fn }
func (s *Store) Root() string               { return s.root }
func FromTimelineDir(dir string) *Store {
	for p := filepath.Clean(dir); p != "." && p != filepath.Dir(p); p = filepath.Dir(p) {
		if filepath.Base(p) == "logs" {
			return ForWorkDir(filepath.Dir(p))
		}
	}
	return nil
}

type contextKey struct{}

func WithStore(ctx context.Context, s *Store) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) *Store { s, _ := ctx.Value(contextKey{}).(*Store); return s }

type Instruction struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Prompt struct {
	CallID          string        `json:"call_id,omitempty"`
	RecordingStatus string        `json:"recording_status"`
	Representation  string        `json:"representation"`
	Messages        []Instruction `json:"messages"`
	Instructions    string        `json:"instructions,omitempty"`
}
type Call struct {
	Kind                  string     `json:"kind,omitempty"`
	ToolName              string     `json:"tool_name,omitempty"`
	Arguments             string     `json:"arguments,omitempty"`
	CallID                string     `json:"call_id"`
	RequestID             string     `json:"request_id"`
	RequestIDs            []string   `json:"request_ids,omitempty"`
	SessionID             string     `json:"session_id"`
	RunID                 string     `json:"run_id,omitempty"`
	AgentID               string     `json:"agent_id,omitempty"`
	DispatchID            string     `json:"dispatch_id,omitempty"`
	ParentDispatchID      string     `json:"parent_dispatch_id,omitempty"`
	ToolCallID            string     `json:"tool_call_id,omitempty"`
	ProviderID            string     `json:"provider_id"`
	ModelID               string     `json:"model_id"`
	UsageType             string     `json:"usage_type"`
	Status                string     `json:"status"`
	StartedAt             time.Time  `json:"started_at"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
	DurationMS            int64      `json:"duration_ms"`
	AttemptCount          int        `json:"attempt_count"`
	RetryCount            int        `json:"retry_count"`
	AttemptsReported      bool       `json:"attempts_reported"`
	PromptTokens          int        `json:"prompt_tokens"`
	CompletionTokens      int        `json:"completion_tokens"`
	TotalTokens           int        `json:"total_tokens"`
	PromptAvailable       bool       `json:"prompt_available"`
	PromptHash            string     `json:"-"`
	PromptRecordingStatus string     `json:"-"`
	PromptSourceCallID    string     `json:"prompt_source_call_id,omitempty"`
	MetricsOnly           bool       `json:"metrics_only,omitempty"`
	RecordingStatus       string     `json:"recording_status"`
	Content               string     `json:"content,omitempty"`
	Reasoning             string     `json:"reasoning,omitempty"`
	Error                 string     `json:"error,omitempty"`
}

// diskCall keeps snapshot identity private; the API never accepts snapshot paths.
type diskCall struct {
	Call
	Hash         string `json:"prompt_hash,omitempty"`
	PromptStatus string `json:"prompt_recording_status,omitempty"`
}
type Request struct {
	MetricsBounded         bool       `json:"-"`
	MetricsStartedAt       time.Time  `json:"-"`
	MetricsFinishedAt      *time.Time `json:"-"`
	Generation             string     `json:"generation,omitempty"`
	JoinedGeneration       string     `json:"joined_generation,omitempty"`
	JoinedGenerationPinned bool       `json:"joined_generation_pinned,omitempty"`
	SessionID              string     `json:"session_id"`
	RequestID              string     `json:"request_id"`
	Status                 string     `json:"status"`
	RecordingStatus        string     `json:"recording_status"`
	StartedAt              time.Time  `json:"started_at"`
	FinishedAt             *time.Time `json:"finished_at,omitempty"`
	DurationMS             int64      `json:"duration_ms"`
	DurationSource         string     `json:"duration_source,omitempty"`
	TimingComplete         bool       `json:"timing_complete"`
	AttemptsReported       bool       `json:"attempts_reported"`
	MetricsOnly            bool       `json:"metrics_only"`
	CallCount              int        `json:"call_count"`
	AttemptCount           int        `json:"attempt_count"`
	Calls                  []Call     `json:"calls"`
	Tools                  []Call     `json:"tools"`
	NextCursor             string     `json:"next_cursor,omitempty"`
	Errors                 []string   `json:"errors"`
	JoinedTo               string     `json:"joined_to,omitempty"`
}
type requestMeta struct {
	Request
	FinishedProcess string `json:"process,omitempty"`
}

var processID = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

func key(session, request string) string {
	h := sha256.Sum256([]byte(session + "\x00" + request))
	return hex.EncodeToString(h[:])
}
func validID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "/\\\x00") && id != "." && id != ".."
}
func (s *Store) requestDir(session, request string) string {
	return filepath.Join(s.root, "requests", key(session, request))
}
func readJSON(path string, v any) error {
	for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if st, e := os.Lstat(parent); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink observation path")
		}
	}
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > 64<<20 {
		return fmt.Errorf("invalid observation file")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func safeDirs(path string) error {
	p := filepath.Clean(path)
	for q := p; q != filepath.Dir(q); q = filepath.Dir(q) {
		st, e := os.Lstat(q)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("observation directory is a symlink")
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return os.MkdirAll(p, 0700)
}
func writeJSON(path string, v any) error {
	if err := safeDirs(filepath.Dir(path)); err != nil {
		return err
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(name, path)
	}
	return e
}
func (s *Store) meta(session, request string) requestMeta {
	m := requestMeta{Request: Request{SessionID: session, RequestID: request, Status: "unrecorded", RecordingStatus: "unrecorded", Calls: []Call{}, Errors: []string{}}}
	if err := readJSON(filepath.Join(s.requestDir(session, request), "meta.json"), &m); err != nil && !os.IsNotExist(err) {
		m.Status = "error"
		m.RecordingStatus = "partial"
		m.Errors = []string{"request metadata unreadable"}
	}
	return m
}
func (s *Store) Start(session, request string) error {
	if !validID(session) || !validID(request) {
		return nil
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	m := s.meta(session, request)
	if len(m.Errors) > 0 {
		return errors.New("request metadata unreadable")
	}
	if m.Status != "unrecorded" {
		return nil
	}
	m.Status = "running"
	m.RecordingStatus = "complete"
	m.StartedAt = time.Now().UTC()
	m.FinishedProcess = processID
	return writeJSON(filepath.Join(s.requestDir(session, request), "meta.json"), m)
}

// Begin admits a new execution, while Start remains idempotent for each call.
// Completed attempts retain their detail directory and aliases remain pinned to it.
func (s *Store) Begin(session, request string) error {
	if !validID(session) || !validID(request) {
		return nil
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	m := s.meta(session, request)
	if len(m.Errors) > 0 {
		return errors.New("request metadata unreadable")
	}
	if m.Generation != "" && !snapshotID(m.Generation) {
		return errors.New("invalid execution generation")
	}
	if (m.Status == "running" || m.Status == "queued") && m.FinishedProcess == processID {
		return nil
	}
	if m.Status != "unrecorded" {
		old, err := s.requestLocked(session, request)
		if err != nil {
			return err
		}
		m.CallCount = old.CallCount
		m.AttemptCount = old.AttemptCount
		name := m.Generation
		if name == "" {
			name = "legacy"
		}
		if err := writeJSON(filepath.Join(s.requestDir(session, request), "generations", name+".json"), m); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	m = requestMeta{Request: Request{SessionID: session, RequestID: request, Generation: key(session, now.Format(time.RFC3339Nano)), Status: "running", RecordingStatus: "complete", StartedAt: now}, FinishedProcess: processID}
	return writeJSON(filepath.Join(s.requestDir(session, request), "meta.json"), m)
}
func (s *Store) callsDir(session, request, generation string) string {
	dir := filepath.Join(s.requestDir(session, request), "calls")
	if generation != "" {
		if !snapshotID(generation) {
			return filepath.Join(dir, "invalid-generation")
		}
		return filepath.Join(dir, generation)
	}
	return dir
}
func (s *Store) sourceMeta(session string, m requestMeta) (requestMeta, error) {
	source := s.meta(session, m.JoinedTo)
	if m.JoinedGenerationPinned && source.Generation != m.JoinedGeneration {
		name := m.JoinedGeneration
		if name == "" {
			name = "legacy"
		}
		if name != "legacy" && !snapshotID(name) {
			return requestMeta{}, errors.New("invalid source generation")
		}
		err := readJSON(filepath.Join(s.requestDir(session, m.JoinedTo), "generations", name+".json"), &source)
		return source, err
	}
	return source, nil
}
func snapshotID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func (s *Store) Finish(session, request, status string) error {
	if !validID(session) || !validID(request) {
		return nil
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	m := s.meta(session, request)
	if m.Status == "unrecorded" {
		return nil
	}
	if status == "queued" {
		m.Status = status
	} else {
		if status == "" {
			status = "completed"
		}
		m.Status = status
		t := time.Now().UTC()
		m.FinishedAt = &t
		m.DurationMS = t.Sub(m.StartedAt).Milliseconds()
	}
	return writeJSON(filepath.Join(s.requestDir(session, request), "meta.json"), m)
}
func bytesIn(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			if i, e := d.Info(); e == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}
func (s *Store) SaveCall(c Call, p *Prompt) error {
	if !validID(c.CallID) || !validID(c.SessionID) || !validID(c.RequestID) {
		return nil
	}
	diskMu.Lock()
	defer diskMu.Unlock()
	m := s.meta(c.SessionID, c.RequestID)
	dir := s.callsDir(c.SessionID, c.RequestID, m.Generation)
	if m.Generation != "" && c.StartedAt.Before(m.StartedAt) {
		return errors.New("call belongs to a previous execution generation")
	}
	if m.RecordingStatus == "expired" {
		return errors.New("execution details expired")
	}
	if c.RecordingStatus == "" {
		c.RecordingStatus = "complete"
	}
	dc := diskCall{Call: c, Hash: c.PromptHash, PromptStatus: c.PromptRecordingStatus}
	var prior diskCall
	_ = readJSON(filepath.Join(dir, c.CallID+".json"), &prior)
	if dc.Kind == "tool" && prior.Kind == "tool" {
		// Async completion is captured at execution end; the resumed parent can
		// replay its UI completion event later without replacing authoritative failure/timing.
		if prior.Status != "running" {
			return nil
		}
		dc.Arguments = prior.Arguments
		dc.StartedAt = prior.StartedAt
		if prior.RecordingStatus == "partial" {
			dc.RecordingStatus = "partial"
		}
	}
	if dc.Hash == "" {
		dc.Hash = prior.Hash
		dc.PromptAvailable = prior.PromptAvailable
		dc.PromptStatus = prior.PromptStatus
	}
	var promptBytes []byte
	pendingHash := ""
	if p != nil {
		var err error
		promptBytes, err = json.Marshal(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(promptBytes)
		pendingHash = hex.EncodeToString(h[:])
		dc.Hash = pendingHash
		dc.PromptAvailable = true
		dc.PromptStatus = p.RecordingStatus
	}
	path := filepath.Join(dir, c.CallID+".json")
	budget := s.prepareCallBudget(c.SessionID, c.RequestID, path, prior, pendingHash, int64(len(promptBytes)))
	policy := s.effectivePolicy()
	if dc.Kind == "tool" && !budget.fits(policy, dc) {
		dc.Arguments = ""
		dc.Content = ""
		dc.Error = ""
		dc.RecordingStatus = "partial"
		if !budget.fits(policy, dc) {
			m.RecordingStatus = "partial"
			_ = writeJSON(filepath.Join(s.requestDir(c.SessionID, c.RequestID), "meta.json"), m)
			return errors.New("tool detail budget exceeded")
		}
	}
	if !budget.fits(policy, dc) {
		originalContent, originalReasoning := dc.Content, dc.Reasoning
		dc.Content = ""
		dc.Reasoning = ""
		dc.RecordingStatus = "partial"
		if !budget.fits(policy, dc) {
			dc.Hash = ""
			dc.PromptAvailable = false
			dc.Content = originalContent
			dc.Reasoning = originalReasoning
			if !budget.fits(policy, dc) {
				dc.Content = ""
				dc.Reasoning = ""
			}
		}
	}
	// Both files are admitted against the proposed final state under diskMu. No existence shortcut bypasses turn costs.
	if dc.Hash == pendingHash && pendingHash != "" && !budget.pendingExists {
		if err := writeJSON(filepath.Join(s.root, "prompts", pendingHash+".json"), p); err != nil {
			return err
		}
	}
	if e := writeJSON(filepath.Join(dir, c.CallID+".json"), dc); e != nil {
		return e
	}
	m = s.meta(c.SessionID, c.RequestID)
	if m.Status == "unrecorded" {
		m.Status = "running"
		m.StartedAt = c.StartedAt
		m.FinishedProcess = processID
		m.RecordingStatus = "complete"
	}
	if dc.RecordingStatus == "partial" {
		m.RecordingStatus = "partial"
	}
	if e := writeJSON(filepath.Join(s.requestDir(c.SessionID, c.RequestID), "meta.json"), m); e != nil {
		return e
	}
	for _, id := range c.RequestIDs {
		if id == c.RequestID || !validID(id) {
			continue
		}
		alias := s.meta(c.SessionID, id)
		alias.JoinedTo = c.RequestID
		alias.JoinedGeneration = m.Generation
		alias.JoinedGenerationPinned = true

		if e := writeJSON(filepath.Join(s.requestDir(c.SessionID, id), "meta.json"), alias); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Request(session, request string) (Request, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	return s.requestLocked(session, request)
}
func (s *Store) requestLocked(session, request string) (Request, error) {
	if !validID(session) || !validID(request) {
		return Request{}, errors.New("invalid request identity")
	}
	m := s.meta(session, request)
	r := m.Request
	r.Calls = []Call{}
	r.Tools = []Call{}
	if r.Errors == nil {
		r.Errors = []string{}
	}
	source := request
	sourceGeneration := m.Generation
	r.MetricsBounded = m.Generation != ""
	r.MetricsStartedAt = m.StartedAt
	r.MetricsFinishedAt = m.FinishedAt
	if m.JoinedTo != "" {
		source = m.JoinedTo
		sourceMeta, err := s.sourceMeta(session, m)
		if err != nil {
			return r, err
		}
		sourceGeneration = sourceMeta.Generation
		r.MetricsBounded = m.JoinedGenerationPinned
		r.MetricsStartedAt = sourceMeta.StartedAt
		r.MetricsFinishedAt = sourceMeta.FinishedAt
		if sourceMeta.RecordingStatus == "expired" {
			r.RecordingStatus = "expired"
			r.CallCount = sourceMeta.CallCount
			r.AttemptCount = sourceMeta.AttemptCount
		}
	}
	paths, e := filepath.Glob(filepath.Join(s.callsDir(session, source, sourceGeneration), "*.json"))
	if e != nil {
		return r, e
	}
	for _, p := range paths {
		var dc diskCall
		if e := readJSON(p, &dc); e != nil {
			r.Errors = append(r.Errors, "call record unreadable")
			r.RecordingStatus = "partial"
			continue
		}
		if dc.Status == "running" && m.FinishedProcess != processID {
			dc.Status = "interrupted"
		}
		if dc.Kind == "tool" {
			if dc.Status == "running" && m.Status != "running" && m.Status != "queued" {
				dc.Status = "interrupted"
			}
			r.Tools = append(r.Tools, dc.Call)
			continue
		}
		// Only compare snapshots that still exist; never infer equality across a missing file.
		dc.PromptAvailable = false
		if decoded, err := hex.DecodeString(dc.Hash); err == nil && len(decoded) == 32 {
			dir, de := os.Lstat(filepath.Join(s.root, "prompts"))
			st, se := os.Lstat(filepath.Join(s.root, "prompts", dc.Hash+".json"))
			if de == nil && dir.IsDir() && dir.Mode()&os.ModeSymlink == 0 && se == nil && st.Mode().IsRegular() {
				dc.PromptAvailable = true
				dc.PromptHash = dc.Hash
				dc.PromptRecordingStatus = dc.PromptStatus
			}
		}
		r.Calls = append(r.Calls, dc.Call)
		if m.RecordingStatus != "expired" {
			r.AttemptCount += dc.AttemptCount
		}
	}
	sort.Slice(r.Tools, func(i, j int) bool { return r.Tools[i].StartedAt.Before(r.Tools[j].StartedAt) })
	sort.Slice(r.Calls, func(i, j int) bool { return r.Calls[i].StartedAt.Before(r.Calls[j].StartedAt) })
	if len(r.Calls) > 0 {
		r.CallCount = len(r.Calls)
	}
	if r.Status == "running" || r.Status == "queued" {
		if m.FinishedProcess != processID {
			r.Status = "interrupted"
		} else {
			r.DurationMS = time.Since(r.StartedAt).Milliseconds()
		}
	}
	return r, nil
}
func (s *Store) CallPrompt(session, request, id string) (Prompt, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	p := Prompt{CallID: id, RecordingStatus: "unrecorded", Representation: "messages", Messages: []Instruction{}}
	if !validID(id) || !validID(session) || !validID(request) {
		return p, errors.New("invalid identity")
	}
	m := s.meta(session, request)
	if m.RecordingStatus == "expired" {
		p.RecordingStatus = "expired"
		return p, nil
	}
	source := request
	sourceGeneration := m.Generation
	if m.JoinedTo != "" {
		source = m.JoinedTo
		sourceMeta, err := s.sourceMeta(session, m)
		if err != nil {
			return p, err
		}
		sourceGeneration = sourceMeta.Generation
		if sourceMeta.RecordingStatus == "expired" {
			p.RecordingStatus = "expired"
			return p, nil
		}
	}
	var dc diskCall
	if e := readJSON(filepath.Join(s.callsDir(session, source, sourceGeneration), id+".json"), &dc); e != nil {
		if os.IsNotExist(e) {
			return p, nil
		}
		return p, e
	}
	if _, err := hex.DecodeString(dc.Hash); err != nil {
		return p, errors.New("invalid prompt snapshot identity")
	}
	if len(dc.Hash) != 64 {
		return p, nil
	}
	if e := readJSON(filepath.Join(s.root, "prompts", dc.Hash+".json"), &p); e != nil {
		if os.IsNotExist(e) {
			p.RecordingStatus = "expired"
			return p, nil
		}
		return p, e
	}
	p.CallID = id
	if p.Messages == nil {
		p.Messages = []Instruction{}
	}
	return p, nil
}

type RetentionUnit struct {
	ID         string
	Bytes      int64
	FinishedAt time.Time
	Active     bool
}

func (s *Store) RetentionUnits() ([]RetentionUnit, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	entries, e := os.ReadDir(filepath.Join(s.root, "requests"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var out []RetentionUnit
	for _, en := range entries {
		if !en.IsDir() || len(en.Name()) != 64 {
			continue
		}
		var m requestMeta
		dir := filepath.Join(s.root, "requests", en.Name())
		if e := readJSON(filepath.Join(dir, "meta.json"), &m); e != nil {
			continue
		}
		active := (m.Status == "running" || m.Status == "queued") && m.FinishedProcess == processID
		end := m.StartedAt
		if m.FinishedAt != nil {
			end = *m.FinishedAt
		}
		out = append(out, RetentionUnit{ID: en.Name(), Bytes: bytesIn(filepath.Join(dir, "calls")), FinishedAt: end, Active: active})
	}
	return out, nil
}

// RemovalStats reports physical files successfully removed and the net bytes released.
// Rewriting retained summary metadata may reduce the net bytes released.
type RemovalStats struct {
	DeletedFiles int
	FreedBytes   int64
}

func (s *Store) Expire(id string) error {
	_, err := s.ExpireWithStats(id)
	return err
}
func (s *Store) ExpireWithStats(id string) (stats RemovalStats, retErr error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	if len(id) != 64 {
		return stats, errors.New("invalid retention identity")
	}
	if _, e := hex.DecodeString(id); e != nil {
		return stats, e
	}
	dir := filepath.Join(s.root, "requests", id)
	before := bytesIn(dir)
	defer func() { stats.FreedBytes = before - bytesIn(dir) }()
	var m requestMeta
	if e := readJSON(filepath.Join(dir, "meta.json"), &m); e != nil {
		return stats, e
	}
	if (m.Status == "running" || m.Status == "queued") && m.FinishedProcess == processID {
		return stats, ErrActive
	}
	r, e := s.requestLocked(m.SessionID, m.RequestID)
	if e != nil {
		return stats, e
	}
	m.CallCount = r.CallCount
	m.AttemptCount = r.AttemptCount
	m.RecordingStatus = "expired"
	if m.Status == "running" || m.Status == "queued" {
		m.Status = "interrupted"
	}
	if e = writeJSON(filepath.Join(dir, "meta.json"), m); e != nil {
		return stats, e
	}
	generations, err := filepath.Glob(filepath.Join(dir, "generations", "*.json"))
	if err != nil {
		return stats, err
	}
	for _, path := range generations {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		if name != "legacy" && !snapshotID(name) {
			continue
		}
		var archived requestMeta
		if err := readJSON(path, &archived); err != nil {
			return stats, err
		}
		archived.RecordingStatus = "expired"
		if err := writeJSON(path, archived); err != nil {
			return stats, err
		}
	}
	// Remove individual owned files so partial failures are never credited as a whole unit.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return stats, err
	}
	defer root.Close()
	err = filepath.WalkDir(filepath.Join(dir, "calls"), func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("unsafe execution detail file")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if err = root.Remove(rel); err != nil {
			return err
		}
		stats.DeletedFiles++
		return nil
	})
	if err == nil {
		err = root.RemoveAll("calls")
	}
	return stats, err
}
func (s *Store) PrunePrompts() error {
	_, err := s.PrunePromptsWithStats()
	return err
}
func (s *Store) PrunePromptsWithStats() (stats RemovalStats, retErr error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	// Validate all existing components without creating missing directories.
	for p := filepath.Join(s.root, "prompts"); p != filepath.Dir(p); p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return stats, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return stats, errors.New("unsafe prompt directory")
		}
	}
	root, err := os.OpenRoot(s.root)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	defer root.Close()
	used := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(s.root, "requests"), func(path string, en os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if en.Type()&os.ModeSymlink != 0 {
			return errors.New("unsafe call directory")
		}
		if en.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(filepath.Join(s.root, "requests"), path)
		if e != nil {
			return e
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if (len(parts) != 3 && len(parts) != 4) || !snapshotID(parts[0]) || parts[1] != "calls" || (len(parts) == 4 && !snapshotID(parts[2])) || !strings.HasSuffix(en.Name(), ".json") {
			return nil
		}
		var c diskCall
		if e := readJSON(path, &c); e != nil {
			return e
		}
		used[c.Hash] = true
		return nil
	})
	if err != nil {
		return stats, err
	}
	directory, err := root.Open("prompts")
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".json") || !snapshotID(strings.TrimSuffix(name, ".json")) {
			continue
		}
		st, e := en.Info()
		if e != nil {
			return stats, e
		}
		if !st.Mode().IsRegular() || used[strings.TrimSuffix(name, ".json")] {
			continue
		}
		if e = root.Remove(filepath.Join("prompts", name)); e != nil {
			return stats, e
		}
		stats.DeletedFiles++
		stats.FreedBytes += st.Size()
	}
	return stats, nil
}

// Resume marks queued inputs as executing without changing their original arrival time.
func (s *Store) Resume(session string, ids []string) error {
	diskMu.Lock()
	defer diskMu.Unlock()
	for _, id := range ids {
		if !validID(id) {
			continue
		}
		m := s.meta(session, id)
		if m.StartedAt.IsZero() {
			m.StartedAt = time.Now().UTC()
		}
		m.Status = "running"
		m.FinishedAt = nil
		m.FinishedProcess = processID
		if e := writeJSON(filepath.Join(s.requestDir(session, id), "meta.json"), m); e != nil {
			return e
		}
	}
	return nil
}
