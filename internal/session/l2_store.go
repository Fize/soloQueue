package session

import (
	"context"
	"sort"
	"strings"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// L2SessionEntry holds a single L2 session with its metadata.
type L2SessionEntry struct {
	ID         string    `json:"id"`         // UUID
	Name       string    `json:"name"`       // auto-generated from first exchange
	Group      string    `json:"group"`      // leader template group
	ProjectID  string    `json:"project_id"` // optional project ID
	WorkDir    string    `json:"work_dir"`   // working directory for agent (defaults to global)
	Session    *Session  `json:"-"`          // the backing Session (nil if not yet activated)
	CreatedAt  time.Time `json:"created_at"` // creation timestamp
	GitBaseRef string    `json:"-"`          // git HEAD at session start (empty = non-git or not captured)
	Plans      []string  `json:"plans,omitempty"` // list of modified plan files
}

// L2SessionInfo is the public metadata returned by List().
type L2SessionInfo struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Group           string    `json:"group"`
	ProjectID       string    `json:"project_id,omitempty"`
	WorkDir         string    `json:"work_dir,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	CtxwinUsed      int       `json:"ctxwin_used"`
	CtxwinLimit     int       `json:"ctxwin_limit"`
	Plans           []string  `json:"plans,omitempty"`
}

// L2SessionStore manages multiple L2 sessions keyed by UUID.
//
// Sessions are explicitly created by the user. Each session has independent
// timeline, context window, and agent. Sessions persist across restarts via
// timeline replay.
type L2SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*L2SessionEntry // key: UUID

	builder       *Builder
	logger        *logger.Logger
	workDir       string
	activeSession *Session
}

// NewL2SessionStore creates a new L2SessionStore.
func NewL2SessionStore(builder *Builder, workDir string, log *logger.Logger) *L2SessionStore {
	return &L2SessionStore{
		sessions: make(map[string]*L2SessionEntry),
		builder:  builder,
		logger:   log,
		workDir:  workDir,
	}
}

// WorkDir returns the store's working directory (for baseline file paths).
func (s *L2SessionStore) WorkDir() string {
	return s.workDir
}

// Create creates a new L2 session entry (metadata only, agent is lazily built).
// The session is NOT activated until the first message is sent via GetOrActivate.
func (s *L2SessionStore) Create(ctx context.Context, id, group, projectID, workDir string) (*L2SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; exists {
		return nil, fmt.Errorf("L2 session %q already exists", id)
	}

	entry := &L2SessionEntry{
		ID:        id,
		Name:      "", // auto-generated after first exchange
		Group:     group,
		ProjectID: projectID,
		WorkDir:   workDir,
		Session:   nil, // built lazily on first use
		CreatedAt: time.Now(),
	}
	s.sessions[id] = entry

	if s.logger != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "L2 session created",
			"id", id,
			"group", group,
		)
	}

	return &L2SessionInfo{
		ID:        entry.ID,
		Name:      entry.Name,
		Group:     entry.Group,
		ProjectID: entry.ProjectID,
		WorkDir:   entry.WorkDir,
		CreatedAt: entry.CreatedAt,
	}, nil
}

// restoreFromDisk attempts to recover an L2 session from its persisted timeline
// metadata on disk. This handles server restarts where in-memory sessions are lost.
func (s *L2SessionStore) restoreFromDisk(ctx context.Context, id string) error {
	if _, err := os.Stat(filepath.Join(s.workDir, "logs", "timelines", "l2-"+id)); err != nil {
		return fmt.Errorf("L2 session %q timeline directory not found", id)
	}

	// LoadMeta migrates any legacy meta/level/baseline/group files into the
	// unified meta.json on first read and removes the originals.
	meta, err := LoadMeta(s.workDir, id)
	if err != nil {
		return fmt.Errorf("L2 session %q: cannot determine group from disk: %w", id, err)
	}

	// If name is empty, try to resolve it from the timeline.
	// We scan files oldest-first and pick the first non-ephemeral real user message
	// (skipping [Delegation Completed] and other ephemeral messages that carry role=user).
	if meta.Name == "" {
		if resolved := ResolveSessionNameFromTimeline(s.workDir, id); resolved != "" {
			resolvedName := resolved
			if err := MergeAndSave(s.workDir, id, func(m *SessionMeta) {
				m.Name = resolvedName
			}); err != nil && s.logger != nil {
				s.logger.WarnContext(ctx, logger.CatApp, "L2 session name backfill to meta.json failed",
					"id", id, "err", err.Error())
			}
			meta.Name = resolvedName
		}
	}

	// Create in-memory entry under write lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[id]; exists {
		return nil // race: someone else created or restored it already
	}
	var plans []string
	if len(meta.Plans) > 0 {
		plans = append(plans, meta.Plans...)
	}
	s.sessions[id] = &L2SessionEntry{
		ID:         id,
		Name:       meta.Name,
		Group:      meta.Group,
		WorkDir:    meta.WorkDir,
		Session:    nil, // will be built lazily by Activate
		CreatedAt:  time.Now(),
		GitBaseRef: meta.GitBaseRef,
		Plans:      plans,
	}

	if s.logger != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "L2 session restored from disk",
			"id", id,
			"group", meta.Group,
		)
	}

	return nil
}

// ResolveSessionNameFromTimeline scans the timeline for the first non-ephemeral
// user message and returns a short display name derived from it. Exported so
// the session list handler can reuse the same logic when backfilling names.
func ResolveSessionNameFromTimeline(workDir, id string) string {
	tlDir := filepath.Join(workDir, "logs", "timelines", "l2-"+id)
	files, _ := timeline.ListTimelineFiles(tlDir, "timeline")
	for _, f := range files {
		events, err := timeline.ReadFileEvents(f)
		if err != nil {
			continue
		}
		for _, evt := range events {
			if evt.EventType != timeline.EventMessage || evt.Message == nil {
				continue
			}
			msg := evt.Message
			if msg.Role != "user" || msg.Content == "" {
				continue
			}
			if msg.IsEphemeral {
				continue
			}
			if strings.HasPrefix(msg.Content, "[Delegation Completed]") {
				continue
			}
			name := msg.Content
			if idx := strings.Index(name, "\n"); idx != -1 {
				name = name[:idx]
			}
			name = strings.TrimSpace(name)
			if len([]rune(name)) > 30 {
				name = string([]rune(name)[:27]) + "..."
			}
			if name != "" {
				return name
			}
		}
	}
	return ""
}

// Activate builds the backing Session for an L2 session entry.
// Call when the user sends the first message to this session.
func (s *L2SessionStore) Activate(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	entry, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("L2 session %q not found", id)
	}

	if entry.Session != nil {
		sess := entry.Session
		s.mu.Unlock()
		return sess, nil
	}
	// Store metadata before unlocking (entry is still valid).
	group := entry.Group
	workDir := entry.WorkDir
	s.mu.Unlock()

	// Build the session outside the lock (may take time).
	sess, err := s.builder.BuildL2(ctx, id, group, workDir)
	if err != nil {
		return nil, fmt.Errorf("activate L2 session %q: %w", id, err)
	}

	s.mu.Lock()
	// Re-check entry — it may have been removed or activated concurrently.
	if e, ok := s.sessions[id]; ok {
		e.Session = sess
		// Pull baseline info from the just-built session instead of re-reading
		// meta.json. BuildL2 captured the baseline and exposed it on the
		// session; this is cheaper than a disk round-trip and avoids the
		// historical self-read loop.
		if e.GitBaseRef == "" {
			e.GitBaseRef = sess.gitBaseRef
		}
	}
	s.mu.Unlock()

	if s.logger != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "L2 session activated",
			"id", id,
			"group", group,
		)
	}

	return sess, nil
}

// Get returns an active L2 session by ID. If the session exists but is not yet
// activated, it activates it automatically.
func (s *L2SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	s.mu.RLock()
	entry, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		// Session not in memory — try restoring from disk timeline metadata.
		// This handles server restarts where in-memory sessions are lost.
		if err := s.restoreFromDisk(ctx, id); err != nil {
			return nil, fmt.Errorf("L2 session %q not found", id)
		}
		// Entry should now exist after restoration.
		s.mu.RLock()
		entry, ok = s.sessions[id]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("L2 session %q not found", id)
		}
	}

	if entry.Session != nil {
		return entry.Session, nil
	}

	return s.Activate(ctx, id)
}

// SetName updates the display name of an L2 session.
func (s *L2SessionStore) SetName(id, name string) {
	s.mu.Lock()
	entry, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	entry.Name = name
	if s.logger != nil {
		s.logger.DebugContext(context.Background(), logger.CatApp, "L2 session renamed",
			"id", id,
			"name", name,
		)
	}

	// Snapshot the in-memory fields we want to durably seed the merge with.
	// This protects against a missing meta.json on disk (e.g. the test that
	// calls SetName before BuildL2 has run) — without these, MergeAndSave
	// would start from a zero-value SessionMeta whose Group is empty, and
	// the next LoadMeta would refuse to restore the session.
	workDir := s.workDir
	seed := newL2EntrySeed(entry)
	s.mu.Unlock()

	if err := MergeAndSave(workDir, id, func(m *SessionMeta) {
		applyEntrySeed(m, seed)
		m.Name = name
	}); err != nil && s.logger != nil {
		s.logger.WarnContext(context.Background(), logger.CatApp, "L2 session rename to meta.json failed",
			"id", id, "err", err.Error())
	}
}

// l2EntrySeed captures the in-memory fields that must be present on disk for
// the session to be restorable. It is a value type so the caller can pass it
// outside the L2SessionStore mutex before invoking MergeAndSave.
type l2EntrySeed struct {
	Group      string
	WorkDir    string
	GitBaseRef string
}

func newL2EntrySeed(e *L2SessionEntry) l2EntrySeed {
	return l2EntrySeed{Group: e.Group, WorkDir: e.WorkDir, GitBaseRef: e.GitBaseRef}
}

// applyEntrySeed fills in any field that the caller is about to mutate but
// the disk meta.json is missing. Fields that the meta.json already has are
// left alone.
func applyEntrySeed(m *SessionMeta, s l2EntrySeed) {
	if m.Group == "" {
		m.Group = s.Group
	}
	if m.WorkDir == "" {
		m.WorkDir = s.WorkDir
	}
	if m.GitBaseRef == "" {
		m.GitBaseRef = s.GitBaseRef
	}
}

// GetName returns the current display name of an L2 session.
func (s *L2SessionStore) GetName(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if entry, ok := s.sessions[id]; ok {
		return entry.Name
	}
	return ""
}

// GetEntry returns a copy of the L2 session entry metadata (without the Session pointer).
// Returns nil if the session is not found (including after disk restore attempt).
func (s *L2SessionStore) GetEntry(id string) *L2SessionEntry {
	s.mu.RLock()
	entry, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		// Try restoring from disk.
		if err := s.restoreFromDisk(context.Background(), id); err != nil {
			return nil
		}
		s.mu.RLock()
		entry, ok = s.sessions[id]
		s.mu.RUnlock()
		if !ok {
			return nil
		}
	}
	// Return a copy without the Session pointer.
	return &L2SessionEntry{
		ID:         entry.ID,
		Name:       entry.Name,
		Group:      entry.Group,
		ProjectID:  entry.ProjectID,
		WorkDir:    entry.WorkDir,
		CreatedAt:  entry.CreatedAt,
		GitBaseRef: entry.GitBaseRef,
		Plans:      append([]string(nil), entry.Plans...),
	}
}


// Remove destroys an L2 session: stops the agent, closes the timeline, removes
// the timeline directory from disk, and removes the entry from the store.
func (s *L2SessionStore) Remove(ctx context.Context, id string) error {
	s.mu.Lock()
	entry, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("L2 session %q not found", id)
	}
	delete(s.sessions, id)
	s.mu.Unlock()

	if entry.Session != nil {
		entry.Session.Close()
	}

	// Remove timeline directory from disk.
	tlDir := filepath.Join(s.workDir, "logs", "timelines", "l2-"+id)
	if err := os.RemoveAll(tlDir); err != nil && s.logger != nil {
		s.logger.WarnContext(ctx, logger.CatApp, "L2 session: failed to remove timeline dir",
			"id", id,
			"dir", tlDir,
			"err", err.Error(),
		)
	}

	if s.logger != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "L2 session removed",
			"id", id,
		)
	}

	return nil
}

// DefaultContextLimit resolves the default model context window limit.
func (s *L2SessionStore) DefaultContextLimit() int {
	if s.builder != nil && s.builder.RT != nil {
		dm := s.builder.RT.ReadDefaultModel()
		if dm != nil && dm.ContextWindow > 0 {
			return dm.ContextWindow
		}
	}
	return 1048576 // fallback default
}

// List returns metadata for all L2 sessions, sorted by created_at descending.
func (s *L2SessionStore) List() []L2SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]L2SessionInfo, 0, len(s.sessions))
	for _, entry := range s.sessions {
		agentInstanceID := ""
		if entry.Session != nil && entry.Session.Agent != nil {
			agentInstanceID = entry.Session.Agent.InstanceID
		}
		ctxwinUsed, ctxwinLimit := 0, 0
		if entry.Session != nil && entry.Session.CW() != nil {
			ctxwinUsed, ctxwinLimit, _ = entry.Session.CW().TokenUsage()
		} else {
			ctxwinLimit = s.DefaultContextLimit()
		}
		// Skip sessions that have never been activated (no timeline directory on disk).
		// These are "phantom" sessions created but never used — they should not
		// reappear after the window is closed and reopened while the server is still running.
		tlDir := filepath.Join(s.workDir, "logs", "timelines", "l2-"+entry.ID)
		if _, err := os.Stat(tlDir); err != nil {
			continue
		}
		result = append(result, L2SessionInfo{
			ID:              entry.ID,
			Name:            entry.Name,
			Group:           entry.Group,
			ProjectID:       entry.ProjectID,
			WorkDir:         entry.WorkDir,
			AgentInstanceID: agentInstanceID,
			CreatedAt:       entry.CreatedAt,
			CtxwinUsed:      ctxwinUsed,
			CtxwinLimit:     ctxwinLimit,
			Plans:           append([]string(nil), entry.Plans...),
		})
	}

	// Sort by created_at descending (newest first).
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

// Shutdown stops all L2 sessions and closes their resources.
func (s *L2SessionStore) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.sessions {
		if entry.Session != nil {
			entry.Session.Close()
		}
		if s.logger != nil {
			s.logger.DebugContext(context.Background(), logger.CatApp, "L2 session shut down",
				"id", id,
			)
		}
	}
	s.sessions = make(map[string]*L2SessionEntry)
}

// UpdatePlanStatus adds a path to the session's Plans list if it doesn't already exist.
func (s *L2SessionStore) UpdatePlanStatus(id, path string) {
	s.mu.Lock()
	entry, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	for _, p := range entry.Plans {
		if p == path {
			s.mu.Unlock()
			return // already exists
		}
	}
	entry.Plans = append(entry.Plans, path)
	if s.logger != nil {
		s.logger.DebugContext(context.Background(), logger.CatApp, "L2 session plan updated",
			"id", id,
			"path", path,
		)
	}

	// Persist the updated Plans list through metastore.MergeAndSave so the
	// other fields (name, group, level, baseline) are preserved untouched.
	// Seed the merge with the in-memory entry's identifying fields in case
	// meta.json doesn't exist yet (e.g. when a tool fires before BuildL2 has
	// had a chance to write the initial meta.json).
	workDir := s.workDir
	seed := newL2EntrySeed(entry)
	s.mu.Unlock()

	if err := MergeAndSave(workDir, id, func(m *SessionMeta) {
		applyEntrySeed(m, seed)
		for _, p := range m.Plans {
			if p == path {
				return
			}
		}
		m.Plans = append(m.Plans, path)
	}); err != nil && s.logger != nil {
		s.logger.WarnContext(context.Background(), logger.CatApp, "L2 session plan status to meta.json failed",
			"id", id, "err", err.Error())
	}
}

// FindActiveSessionByAgentID searches all active L2 sessions for one whose leader agent ID matches the target.
func (s *L2SessionStore) FindActiveSessionByAgentID(agentID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.sessions {
		if entry.Session != nil && entry.Session.Agent != nil && entry.Session.Agent.Def.ID == agentID {
			return entry.Session
		}
	}
	return nil
}

// ActiveSession returns the currently active L2 session, or nil.
func (s *L2SessionStore) ActiveSession() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeSession
}

// SetActiveSession marks a session as the currently active L2 session.
func (s *L2SessionStore) SetActiveSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.sessions[id]; ok {
		s.activeSession = entry.Session
	}
}
