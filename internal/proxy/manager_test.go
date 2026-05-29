package proxy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestProxyManager_HealthCheck(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	// Start a dummy test HTTP server
	dummyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dummyServer.Close()

	pm, err := NewProxyManager(tempDir, stateFile)
	if err != nil {
		t.Fatalf("NewProxyManager: %v", err)
	}

	// 1. Add a healthy proxy and an unhealthy proxy
	_, err = pm.AddProxy("healthy-proxy", dummyServer.URL)
	if err != nil {
		t.Fatalf("AddProxy: %v", err)
	}
	_, err = pm.AddProxy("unhealthy-proxy", "http://127.0.0.1:57649") // Unreachable port
	if err != nil {
		t.Fatalf("AddProxy: %v", err)
	}

	// 2. Start the manager (which triggers an immediate check)
	if err := pm.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pm.Shutdown()

	// Initially, healthy-proxy should be healthy
	_, healthy, _ := pm.GetProxyStatus("healthy-proxy")
	if !healthy {
		t.Errorf("expected healthy-proxy to be healthy")
	}

	// Initially, unhealthy-proxy has failCount=1, so it should still be healthy
	_, healthy, _ = pm.GetProxyStatus("unhealthy-proxy")
	if !healthy {
		t.Errorf("expected unhealthy-proxy to be healthy after 1 failure")
	}

	// 3. Trigger checks to reach 3 failures
	pm.CheckHealth() // 2nd failure
	_, healthy, _ = pm.GetProxyStatus("unhealthy-proxy")
	if !healthy {
		t.Errorf("expected unhealthy-proxy to be healthy after 2 failures")
	}

	pm.CheckHealth() // 3rd failure
	_, healthy, _ = pm.GetProxyStatus("unhealthy-proxy")
	if healthy {
		t.Errorf("expected unhealthy-proxy to be unhealthy after 3 failures")
	}

	// 4. Test recovery: change the unhealthy-proxy target to the dummy server, and check health
	pm.mu.Lock()
	entry := pm.proxies["unhealthy-proxy"]
	entry.TargetURL = dummyServer.URL
	pm.proxies["unhealthy-proxy"] = entry
	pm.mu.Unlock()

	pm.CheckHealth() // success
	_, healthy, _ = pm.GetProxyStatus("unhealthy-proxy")
	if !healthy {
		t.Errorf("expected unhealthy-proxy to recover to healthy after success")
	}
}
