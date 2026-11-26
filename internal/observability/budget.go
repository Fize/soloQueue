package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/storagepolicy"
)

var policyRegistry struct {
	sync.RWMutex
	entries []policyEntry
	next    uint64
}

type policyEntry struct {
	id   uint64
	root string
	fn   func() storagepolicy.Policy
}

func canonicalRoot(root string) string {
	if p, e := filepath.EvalSymlinks(root); e == nil {
		return p
	}
	return filepath.Clean(root)
}

// RegisterPolicy scopes the storage configuration to a work-directory lifetime.
// The latest registration wins; every limit is then derived from it by
// storagepolicy, so writers and the cleaner cannot drift apart.
func RegisterPolicy(root string, fn func() storagepolicy.Policy) func() {
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
func (s *Store) effectiveConfig() storagepolicy.Policy {
	root := filepath.Dir(filepath.Dir(s.root))
	policyRegistry.RLock()
	var fn func() storagepolicy.Policy
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
	return storagepolicy.DefaultPolicy()
}

// effectivePolicy is the execution-details view of the configured limits.
func (s *Store) effectivePolicy() Limits {
	return storagepolicy.LimitsFor(s.effectiveConfig(), storagepolicy.ExecutionDetails)
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

// ledger scans the work directory once; usage and turn bytes are read from it.
func (s *Store) ledger() storagepolicy.Ledger {
	return storagepolicy.Scan(filepath.Dir(filepath.Dir(s.root)))
}

// GuardOptional serializes budget assessment and the resulting write across main
// and child stores. The callback MUST NOT call Store methods, which take this
// same lock.
func (s *Store) GuardOptional(session, request string, addition int64, write func(Limits, bool) error) error {
	diskMu.Lock()
	defer diskMu.Unlock()
	limits := s.effectivePolicy()
	l := s.ledger()
	allowed := len(l.Errors) == 0 &&
		(limits.MaxBytes <= 0 || l.Usage()[storagepolicy.ExecutionDetails]+addition <= limits.MaxBytes) &&
		(limits.MaxTurnBytes <= 0 || l.TurnUsage(request)+addition <= limits.MaxTurnBytes)
	return write(limits, allowed)
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
	l := s.ledger()
	view := callBudget{total: l.Usage()[storagepolicy.ExecutionDetails], turn: l.TurnUsage(request, s.meta(session, request).Generation), known: len(l.Errors) == 0, oldHash: prior.Hash, refs: map[string]int{}, sizes: map[string]int64{}, pendingHash: pendingHash, pendingBytes: pendingBytes}
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
func (v callBudget) fits(policy Limits, candidate diskCall) bool {
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
