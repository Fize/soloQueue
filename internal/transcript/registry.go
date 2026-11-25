package transcript

import (
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// Registry owns the Journal of every logical session served by one runtime.
// The server creates it and hands it to the session owners; a Session without
// a registry neither publishes frames nor stamps rows.
type Registry struct {
	mu       sync.Mutex
	journals map[string]*Journal
	cfg      Config
}

// NewRegistry creates an empty registry.
func NewRegistry(cfg Config) *Registry {
	return &Registry{journals: make(map[string]*Journal), cfg: cfg}
}

// Journal returns the journal for key, creating it on first use. A new
// journal continues the sequence after the largest one persisted in
// timelineDir, so ordering keys stay monotonic across restarts.
func (r *Registry) Journal(key, timelineDir string) *Journal {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.journals[key]; ok {
		return j
	}
	j := newJournal(key, MaxPersistedSeq(timelineDir)+1, r.cfg)
	r.journals[key] = j
	return j
}

// Lookup returns an existing journal without creating one.
func (r *Registry) Lookup(key string) (*Journal, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.journals[key]
	return j, ok
}

// MaxPersistedSeq returns the largest sequence stamped on any timeline event in
// dir, or 0 when the directory is empty, missing or has no stamped events.
func MaxPersistedSeq(dir string) uint64 {
	if dir == "" {
		return 0
	}
	events, err := timeline.ReadAllEvents(dir)
	if err != nil {
		return 0
	}
	var max uint64
	for _, evt := range events {
		if evt.Message != nil && evt.Message.Seq > max {
			max = evt.Message.Seq
		}
		if evt.Control != nil && evt.Control.Seq > max {
			max = evt.Control.Seq
		}
	}
	return max
}
