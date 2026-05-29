package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ProxyEntry represents the target URL.
type ProxyEntry struct {
	TargetURL string
}

// ProxyInfo is the public-facing representation of a proxy mapping.
type ProxyInfo struct {
	ID        string `json:"id"`
	TargetURL string `json:"target_url"`
}

// savedProxy is the JSON-serializable form persisted to the state file.
type savedProxy struct {
	ID        string `json:"id"`
	TargetURL string `json:"target_url"`
}

// ProxyManager manages proxy configurations.
type ProxyManager struct {
	mu        sync.RWMutex
	proxies   map[string]ProxyEntry // id → entry
	pathCache map[string]string     // path → id
	stateFile string                // path to JSON persistence file
}

// NewProxyManager creates a ProxyManager.
// stateFile is the path to a JSON file used to persist proxy state across restarts.
func NewProxyManager(configDir string, stateFile string) (*ProxyManager, error) {
	// configDir is unused now, kept for API compat
	return &ProxyManager{
		proxies:   make(map[string]ProxyEntry),
		pathCache: make(map[string]string),
		stateFile: stateFile,
	}, nil
}

// CachePath records that a specific absolute path belongs to a proxy ID.
func (pm *ProxyManager) CachePath(path, id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.pathCache == nil {
		pm.pathCache = make(map[string]string)
	}
	if len(pm.pathCache) > 5000 {
		// Prevent unbounded memory growth; reset if it gets too large
		pm.pathCache = make(map[string]string)
	}
	pm.pathCache[path] = id
}

// GetCachedProxy retrieves the proxy ID for a given path, if it exists.
func (pm *ProxyManager) GetCachedProxy(path string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.pathCache == nil {
		return ""
	}
	return pm.pathCache[path]
}

// Start loads persisted proxy state.
func (pm *ProxyManager) Start() error {
	if err := pm.loadState(); err != nil {
		// Non-fatal: start fresh if state file is missing or corrupt.
		pm.mu.Lock()
		pm.proxies = make(map[string]ProxyEntry)
		pm.mu.Unlock()
	}
	return nil
}

// AddProxy registers a new proxy mapping and persists state.
func (pm *ProxyManager) AddProxy(id string, targetURL string) (int, error) {
	entry := ProxyEntry{TargetURL: targetURL}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.proxies[id] = entry
	_ = pm.saveStateUnlocked() // best-effort persist

	return 0, nil
}

// RemoveProxy deletes a proxy mapping by id and persists state.
func (pm *ProxyManager) RemoveProxy(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, ok := pm.proxies[id]; !ok {
		return fmt.Errorf("proxy %q not found", id)
	}

	delete(pm.proxies, id)
	_ = pm.saveStateUnlocked() // best-effort persist

	return nil
}

// ListProxies returns a snapshot of all currently registered proxy mappings.
func (pm *ProxyManager) ListProxies() []ProxyInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]ProxyInfo, 0, len(pm.proxies))
	for id, entry := range pm.proxies {
		result = append(result, ProxyInfo{
			ID:        id,
			TargetURL: entry.TargetURL,
		})
	}
	return result
}

// Shutdown does nothing anymore, but kept for interface compat.
func (pm *ProxyManager) Shutdown() error {
	return nil
}

// GetProxyTarget returns the target URL for a given ID, or empty string if not found.
func (pm *ProxyManager) GetProxyTarget(id string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if entry, ok := pm.proxies[id]; ok {
		return entry.TargetURL
	}
	return ""
}

// saveStateUnlocked persists the current proxy map to the state file as JSON.
// Caller must hold mu lock.
func (pm *ProxyManager) saveStateUnlocked() error {
	if pm.stateFile == "" {
		return nil
	}

	saved := make([]savedProxy, 0, len(pm.proxies))
	for id, entry := range pm.proxies {
		saved = append(saved, savedProxy{ID: id, TargetURL: entry.TargetURL})
	}

	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pm.stateFile, data, 0o644)
}

// loadState restores proxy mappings from the state file.
func (pm *ProxyManager) loadState() error {
	if pm.stateFile == "" {
		return nil
	}

	data, err := os.ReadFile(pm.stateFile)
	if err != nil {
		return err // file missing is fine, caller handles gracefully
	}

	var saved []savedProxy
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, s := range saved {
		pm.proxies[s.ID] = ProxyEntry{TargetURL: s.TargetURL}
	}

	return nil
}
