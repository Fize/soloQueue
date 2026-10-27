package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var policyRegistry struct {
	sync.RWMutex
	entries []policyEntry
	next    uint64
}

type policyEntry struct {
	id   uint64
	root string
	fn   func() Policy
}

func canonicalRoot(root string) string {
	if p, e := filepath.EvalSymlinks(root); e == nil {
		return p
	}
	return filepath.Clean(root)
}

// RegisterPolicy scopes shared limits to a work-directory lifetime. The latest registration wins.
func RegisterPolicy(root string, fn func() Policy) func() {
	root = canonicalRoot(root)
	policyRegistry.Lock()
	policyRegistry.next++
	id := policyRegistry.next
	policyRegistry.entries = append(policyRegistry.entries, policyEntry{id, root, fn})
	policyRegistry.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			policyRegistry.Lock()
			defer policyRegistry.Unlock()
			for i, e := range policyRegistry.entries {
				if e.id == id {
					policyRegistry.entries = append(policyRegistry.entries[:i], policyRegistry.entries[i+1:]...)
					break
				}
			}
		})
	}
}
func (s *Store) effectivePolicy() Policy {
	root := filepath.Dir(filepath.Dir(s.root))
	policyRegistry.RLock()
	var fn func() Policy
	for i := len(policyRegistry.entries) - 1; i >= 0; i-- {
		if policyRegistry.entries[i].root == root {
			fn = policyRegistry.entries[i].fn
			break
		}
	}
	policyRegistry.RUnlock()
	if fn != nil {
		return fn()
	}
	if s.policy != nil {
		return s.policy()
	}
	return Policy{MaxBytes: 1 << 30, MaxTurnBytes: 50 << 20, MaxFileBytes: 50 << 20}
}
func canonicalSession(id string) string {
	if id == "l1-session" {
		return "l1"
	}
	if strings.HasPrefix(id, "l2-") && strings.HasSuffix(id, "-session") {
		return "l2:" + strings.TrimSuffix(strings.TrimPrefix(id, "l2-"), "-session")
	}
	return id
}

// SharedUsage counts optional detail files in both stores. Recovery snapshots/meta are deliberately excluded.
// An existing legacy journal is counted conservatively as detail data until safe compaction.
func (s *Store) sharedUsage(session, request string) (total, turn int64, complete bool) {
	complete = true
	root := filepath.Dir(filepath.Dir(s.root))
	total, complete = budgetBytes(s.root)
	meta := s.meta(session, request)
	dir := s.callsDir(session, request, meta.Generation)
	turn = bytesIn(dir)
	refs := map[string]bool{}
	calls, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, p := range calls {
		var c diskCall
		if err := readJSON(p, &c); err != nil {
			complete = false
			continue
		}
		if c.Hash != "" && !refs[c.Hash] {
			refs[c.Hash] = true
			if st, e := os.Lstat(filepath.Join(s.root, "prompts", c.Hash+".json")); e == nil && st.Mode().IsRegular() {
				turn += st.Size()
			}
		}
	}
	for _, base := range []string{"timelines", "cron"} {
		_ = filepath.WalkDir(filepath.Join(root, "logs", base), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if !os.IsNotExist(err) {
					complete = false
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !d.IsDir() || !strings.HasPrefix(d.Name(), "dlg_") || filepath.Base(filepath.Dir(path)) != "delegations" {
				return nil
			}
			var rec struct {
				RequestID string    `json:"request_id"`
				Owner     string    `json:"owner_session_id"`
				CreatedAt time.Time `json:"created_at"`
			}
			b, e := os.ReadFile(filepath.Join(path, "meta.json"))
			knownOwner := e == nil && len(b) <= 1<<20 && json.Unmarshal(b, &rec) == nil
			var bytes int64
			entries, readErr := os.ReadDir(path)
			if readErr != nil {
				complete = false
				return filepath.SkipDir
			}
			for _, en := range entries {
				if en.Type()&os.ModeSymlink != 0 || en.IsDir() {
					continue
				}
				if strings.HasPrefix(en.Name(), "stream-") && strings.HasSuffix(en.Name(), ".jsonl") {
					if st, e := en.Info(); e == nil {
						bytes += st.Size()
					}
				}
			}
			total += bytes
			if bytes > 0 && !knownOwner {
				complete = false
			}
			if rec.RequestID == request && canonicalSession(rec.Owner) == canonicalSession(session) && (meta.Generation == "" || !rec.CreatedAt.Before(meta.StartedAt)) {
				turn += bytes
			}
			return filepath.SkipDir
		})
	}
	return
}
func (s *Store) allowShared(session, request string, addition int64) bool {
	p := s.effectivePolicy()
	total, turn, complete := s.sharedUsage(session, request)
	return complete && (p.MaxBytes <= 0 || total+addition <= p.MaxBytes) && (p.MaxTurnBytes <= 0 || turn+addition <= p.MaxTurnBytes)
}

// GuardOptional serializes budget assessment and the resulting write across main and child stores.
// The callback MUST NOT call Store methods, which use this same lock.
func (s *Store) GuardOptional(session, request string, addition int64, write func(Policy, bool) error) error {
	diskMu.Lock()
	defer diskMu.Unlock()
	p := s.effectivePolicy()
	return write(p, s.allowShared(canonicalSession(session), request, addition))
}

func budgetBytes(dir string) (n int64, complete bool) {
	complete = true
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				complete = false
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			complete = false
			return nil
		}
		if !d.IsDir() {
			st, e := d.Info()
			if e != nil {
				complete = false
			} else {
				n += st.Size()
			}
		}
		return nil
	})
	return
}

// callBudget models the replacement's final on-disk state before either prompt or call is written.
// A prompt costs physical bytes globally only when new, but costs one deduplicated reference per turn.
type callBudget struct {
	total, turn, oldFileBytes int64
	known                     bool
	oldHash                   string
	refs                      map[string]int
	sizes                     map[string]int64
	pendingHash               string
	pendingBytes              int64
	pendingExists             bool
}

func (s *Store) prepareCallBudget(session, request, path string, prior diskCall, pendingHash string, pendingBytes int64) callBudget {
	total, turn, known := s.sharedUsage(session, request)
	view := callBudget{total: total, turn: turn, known: known, oldHash: prior.Hash, refs: map[string]int{}, sizes: map[string]int64{}, pendingHash: pendingHash, pendingBytes: pendingBytes}
	if st, e := os.Lstat(path); e == nil && st.Mode().IsRegular() {
		view.oldFileBytes = st.Size()
	} else if e != nil && !os.IsNotExist(e) {
		view.known = false
	}
	files, _ := filepath.Glob(filepath.Join(s.callsDir(session, request, s.meta(session, request).Generation), "*.json"))
	for _, p := range files {
		var call diskCall
		if e := readJSON(p, &call); e != nil {
			view.known = false
			continue
		}
		if call.Hash != "" {
			view.refs[call.Hash]++
		}
	}
	for hash := range view.refs {
		if st, e := os.Lstat(filepath.Join(s.root, "prompts", hash+".json")); e == nil && st.Mode().IsRegular() {
			view.sizes[hash] = st.Size()
		} else {
			view.known = false
		}
	}
	if pendingHash != "" {
		if st, e := os.Lstat(filepath.Join(s.root, "prompts", pendingHash+".json")); e == nil && st.Mode().IsRegular() {
			view.pendingExists = true
			view.pendingBytes = st.Size()
			view.sizes[pendingHash] = st.Size()
		} else if !os.IsNotExist(e) {
			view.known = false
		}
	}
	return view
}
func (v callBudget) fits(policy Policy, candidate diskCall) bool {
	if !v.known {
		return false
	}
	encoded, e := json.Marshal(candidate)
	if e != nil {
		return false
	}
	callBytes := int64(len(encoded))
	if policy.MaxFileBytes > 0 && callBytes > policy.MaxFileBytes {
		return false
	}
	total := v.total - v.oldFileBytes + callBytes
	turn := v.turn - v.oldFileBytes + callBytes
	if candidate.Hash != v.oldHash && v.oldHash != "" && v.refs[v.oldHash] == 1 {
		turn -= v.sizes[v.oldHash]
	}
	if candidate.Hash != "" {
		promptBytes := v.sizes[candidate.Hash]
		if candidate.Hash == v.pendingHash {
			promptBytes = v.pendingBytes
			if !v.pendingExists {
				total += promptBytes
			}
		}
		if promptBytes <= 0 || (policy.MaxFileBytes > 0 && promptBytes > policy.MaxFileBytes) {
			return false
		}
		if candidate.Hash != v.oldHash && v.refs[candidate.Hash] == 0 {
			turn += promptBytes
		}
	}
	return (policy.MaxBytes <= 0 || total <= policy.MaxBytes) && (policy.MaxTurnBytes <= 0 || turn <= policy.MaxTurnBytes)
}
