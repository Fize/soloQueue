package rotating

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Limits is a live, process-local policy for date/size writers.
type Limits struct {
	MaxBytes          int64
	MaxDays, MaxFiles int
}

type policyEntry struct {
	id      uint64
	resolve func(string, string) Limits
}

var policies struct {
	sync.RWMutex
	roots  map[string][]policyEntry
	next   uint64
	active map[string]int
}

// RegisterPolicy installs a resolver scoped to one work directory. The caller owns its lifetime.
func RegisterPolicy(root string, resolve func(string, string) Limits) func() {
	root = filepath.Clean(root)
	policies.Lock()
	if policies.roots == nil {
		policies.roots = make(map[string][]policyEntry)
	}
	policies.next++
	id := policies.next
	policies.roots[root] = append(policies.roots[root], policyEntry{id, resolve})
	policies.Unlock()
	return func() {
		policies.Lock()
		entries := policies.roots[root]
		for i, e := range entries {
			if e.id == id {
				entries = append(entries[:i], entries[i+1:]...)
				break
			}
		}
		if len(entries) == 0 {
			delete(policies.roots, root)
		} else {
			policies.roots[root] = entries
		}
		policies.Unlock()
	}
}
func resolvePolicy(dir, base string) (Limits, bool) {
	policies.RLock()
	defer policies.RUnlock()
	best := ""
	var fn func(string, string) Limits
	for r, f := range policies.roots {
		if (dir == r || strings.HasPrefix(dir, r+string(filepath.Separator))) && len(r) > len(best) {
			best = r
			fn = f[len(f)-1].resolve
		}
	}
	if fn == nil {
		return Limits{}, false
	}
	return fn(dir, base), true
}
func markActive(path string, delta int) {
	policies.Lock()
	defer policies.Unlock()
	if policies.active == nil {
		policies.active = make(map[string]int)
	}
	policies.active[path] += delta
	if policies.active[path] <= 0 {
		delete(policies.active, path)
	}
}

// IsActive protects file handles held by another writer during cleanup.
func IsActive(path string) bool {
	policies.RLock()
	defer policies.RUnlock()
	return policies.active[filepath.Clean(path)] > 0
}

// RemoveInactive serializes unlink with writer registration so a newly opened file cannot be removed underneath it.
func RemoveInactive(path string, remove func() error) error {
	policies.Lock()
	defer policies.Unlock()
	if policies.active[filepath.Clean(path)] > 0 {
		return fmt.Errorf("file is active")
	}
	return remove()
}
