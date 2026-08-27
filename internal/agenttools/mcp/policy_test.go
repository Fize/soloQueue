package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func TestPolicyLifecycleBindsHostApprovalToDefinition(t *testing.T) {
	db, err := db.Open(filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewPolicyStore(db)
	cfg := ServerConfig{
		Name: "browser", Command: "browser-mcp", Args: []string{"--stdio"},
		Env: map[string]string{"TOKEN": "secret"}, Transport: "stdio", Enabled: true,
	}
	ctx := context.Background()

	policy, err := store.Effective(ctx, "global", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != PolicyNeedsReview {
		t.Fatalf("missing policy = %#v", policy)
	}

	policy, err = store.Approve(ctx, "global", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != PolicyApproved || policy.Revision != 1 {
		t.Fatalf("approved policy = %#v", policy)
	}

	changed := cfg
	changed.Args = []string{"--stdio", "--new-permission"}
	policy, err = store.Effective(ctx, "global", changed)
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != PolicyNeedsReview || policy.ApprovedAt != "" {
		t.Fatalf("changed definition should need review: %#v", policy)
	}

	policy, err = store.Approve(ctx, "global", changed)
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != PolicyApproved || policy.Revision != 2 {
		t.Fatalf("reapproved policy = %#v", policy)
	}

	if err := store.Revoke(ctx, "global", cfg.Name); err != nil {
		t.Fatal(err)
	}
	policy, err = store.Effective(ctx, "global", changed)
	if err != nil {
		t.Fatal(err)
	}
	if policy.State != PolicyRevoked || policy.Revision != 3 {
		t.Fatalf("revoked policy = %#v", policy)
	}
}

func TestDefinitionDigestIsStableAcrossEnvironmentMapOrder(t *testing.T) {
	first := ServerConfig{
		Name: "test", Command: "mcp", Env: map[string]string{"A": "1", "B": "2"},
		Transport: "stdio", Enabled: true,
	}
	second := first
	second.Env = map[string]string{"B": "2", "A": "1"}
	if DefinitionDigest(first) != DefinitionDigest(second) {
		t.Fatal("definition digest depends on map iteration order")
	}
}
