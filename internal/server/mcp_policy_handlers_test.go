package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

func TestMCPPolicyHandlersRequireExplicitHostApprovalAndInvalidateChanges(t *testing.T) {
	dir := t.TempDir()
	db, err := db.Open(filepath.Join(dir, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	loader, err := mcp.NewLoader(filepath.Join(dir, "mcp.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}
	if err := loader.Set(func(cfg *mcp.Config) {
		cfg.Servers = []mcp.ServerConfig{{
			Name: "browser", Command: "browser-mcp", Transport: "stdio", Enabled: true,
		}}
	}); err != nil {
		t.Fatal(err)
	}
	runtimeManager := tools.NewRuntimeManager(tools.RuntimeSandbox, nil)
	manager := mcp.NewManagerWithPolicy(loader, mcp.NewPolicyStore(db), runtimeManager, nil)
	mux := NewMux(dir, nil, WithMCPLoader(loader), WithMCPManager(manager))

	request := newLocalhostRequest(http.MethodGet, "/api/mcp/policies?scope=global", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET policies status = %d body=%s", response.Code, response.Body.String())
	}

	request = newLocalhostRequest(http.MethodPut, "/api/mcp/policies/browser",
		bytes.NewBufferString(`{"scope":"global","runtime":"host"}`))
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("host without confirmation status = %d", response.Code)
	}

	request = newLocalhostRequest(http.MethodPut, "/api/mcp/policies/browser",
		bytes.NewBufferString(`{"scope":"global","runtime":"sandbox"}`))
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sandbox approval status = %d body=%s", response.Code, response.Body.String())
	}
	var approved mcp.Policy
	if err := json.Unmarshal(response.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	if approved.State != mcp.PolicyApproved || approved.Runtime != tools.RuntimeSandbox {
		t.Fatalf("approved policy = %#v", approved)
	}

	if err := loader.Set(func(cfg *mcp.Config) {
		cfg.Servers[0].Args = []string{"--new-capability"}
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := manager.PolicyStore().Effective(context.Background(), "global", loader.Get().Servers[0])
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != mcp.PolicyNeedsReview {
		t.Fatalf("changed definition state = %q", policy.State)
	}

	request = newLocalhostRequest(http.MethodDelete, "/api/mcp/policies/browser?scope=global", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d body=%s", response.Code, response.Body.String())
	}
}
