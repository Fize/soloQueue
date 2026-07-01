package session

import (
	"context"
	"encoding/json"
	"strings"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// L2SessionEntry holds a single L2 session with its metadata.
type L2SessionEntry struct {
	ID        string    `json:"id"`         // UUID
	Name      string    `json:"name"`       // auto-generated from first exchange
	Group     string    `json:"group"`      // leader template group
	ProjectID string    `json:"project_id"` // optional project ID
	WorkDir   string    `json:"work_dir"`   // working directory for agent (defaults to global)
	Session   *Session  `json:"-"`          // the backing Session (nil if not yet activated)
	CreatedAt time.Time `json:"created_at"` // creation timestamp
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
}

// L2SessionStore manages multiple L2 sessions keyed by UUID.
//
// Sessions are explicitly created by the user. Each session has independent
// timeline, context window, and agent. Sessions persist across restarts via
// timeline replay.
type L2SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*L2SessionEntry // key: UUID

	builder  *Builder
	logger   *logger.Logger
	workDir  string
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
	tlDir := filepath.Join(s.workDir, "logs", "timelines", "l2-"+id)
	info, err := os.Stat(tlDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("L2 session %q timeline directory not found", id)
	}

	// Read meta file (preferred) or legacy group file.
	group := ""
	workDir := ""
	name := ""
	metaFile := filepath.Join(tlDir, "meta")
	if data, rerr := os.ReadFile(metaFile); rerr == nil {
		var meta struct {
			Name    string `json:"name"`
			Group   string `json:"group"`
			WorkDir string `json:"work_dir"`
		}
		if json.Unmarshal(data, &meta) == nil {
			name = meta.Name
			group = meta.Group
			workDir = meta.WorkDir
		}
	}
	if group == "" {
		groupFile := filepath.Join(tlDir, "group")
		if data, rerr := os.ReadFile(groupFile); rerr == nil {
			group = strings.TrimSpace(string(data))
		}
	}
	if group == "" {
		return fmt.Errorf("L2 session %q: cannot determine group from disk", id)
	}

	// Create in-memory entry under write lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[id]; exists {
		return nil // race: someone else created or restored it already
	}
	s.sessions[id] = &L2SessionEntry{
		ID:        id,
		Name:      name,
		Group:     group,
		WorkDir:   workDir,
		Session:   nil, // will be built lazily by Activate
		CreatedAt: time.Now(),
	}

	if s.logger != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "L2 session restored from disk",
			"id", id,
			"group", group,
		)
	}

	return nil
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
	if ok {
		entry.Name = name
		if s.logger != nil {
			s.logger.DebugContext(context.Background(), logger.CatApp, "L2 session renamed",
				"id", id,
				"name", name,
			)
		}
		// Write meta to disk
		tlDir := filepath.Join(s.workDir, "logs", "timelines", "l2-"+id)
		metaFile := filepath.Join(tlDir, "meta")
		meta := struct {
			Name    string `json:"name"`
			Group   string `json:"group"`
			WorkDir string `json:"work_dir"`
		}{
			Name:    name,
			Group:   entry.Group,
			WorkDir: entry.WorkDir,
		}
		if data, err := json.Marshal(meta); err == nil {
			_ = os.WriteFile(metaFile, data, 0644)
		}
	}
	s.mu.Unlock()
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
		})
	}

	// Sort by created_at descending (newest first).
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].CreatedAt.After(result[i].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

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
