package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	workdirutil "github.com/xiaobaitu/soloqueue/internal/infra/workdir"
)

func TestDelegateTool_PreferredTimeout_Explicit(t *testing.T) {
	dt := NewDelegateTool("leader", 20*time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 20*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 20m", got)
	}
}

func TestDelegateTool_PreferredTimeout_Default(t *testing.T) {
	dt := NewDelegateTool("leader", 0, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != DelegateDefaultTimeout {
		t.Errorf("PreferredTimeout() = %v, want DelegateDefaultTimeout (%v)", got, DelegateDefaultTimeout)
	}
}

func TestDelegateTool_PreferredTimeout_Capped(t *testing.T) {
	// PreferredTimeout returns the raw dt.Timeout / DelegateDefaultTimeout;
	// the actual capping to DelegateMaxTimeout happens inside Execute/ExecuteAsync.
	dt := NewDelegateTool("leader", 99*time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 99*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 99m (uncapped)", got)
	}
}

func TestDelegateTool_InheritOnlySchemaAndResolution(t *testing.T) {
	dt := NewDelegateTool("worker", time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if strings.Contains(string(dt.Parameters()), "work_dir") {
		t.Fatal("inherit-only schema must not expose work_dir")
	}

	ctx := iface.ContextWithWorkDir(context.Background(), "/parent/project")
	got, err := dt.resolveWorkDir(ctx, "/wrong/project")
	if err != nil {
		t.Fatalf("resolveWorkDir: %v", err)
	}
	if got != "/parent/project" {
		t.Fatalf("resolveWorkDir = %q, want parent directory", got)
	}
}

func TestDelegateTool_ExplicitOrInheritedResolution(t *testing.T) {
	dt := NewDelegateTool("leader", time.Minute, nil, nil, nil, WorkDirExplicitOrInherited)
	if !strings.Contains(string(dt.Parameters()), "work_dir") {
		t.Fatal("explicit schema should expose optional work_dir")
	}

	parentDir := t.TempDir()
	selectedDir := t.TempDir()
	ctx := iface.ContextWithWorkDir(context.Background(), parentDir)
	wantSelected, err := workdirutil.NormalizeExistingDir(selectedDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dt.resolveWorkDir(ctx, selectedDir); err != nil || got != wantSelected {
		t.Fatalf("explicit resolveWorkDir = %q, %v", got, err)
	}
	if got, err := dt.resolveWorkDir(ctx, ""); err != nil || got != parentDir {
		t.Fatalf("inherited resolveWorkDir = %q, %v", got, err)
	}
}

func TestDelegateTool_ExecuteAsyncAlwaysAsyncRejectsCycle(t *testing.T) {
	var resolverCalls atomic.Int32
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		resolverCalls.Add(1)
		return nil, false, nil
	}
	dt := NewDelegateTool(
		"research",
		time.Minute,
		resolver,
		nil,
		nil,
		WorkDirInheritOnly,
		WithAlwaysAsyncDelegation(),
	)
	ctx := ContextWithDelegationChain(
		iface.ContextWithWorkDir(context.Background(), "/parent/project"),
		[]string{"engineering", "research"},
	)

	action, err := dt.ExecuteAsync(ctx, `{"target":"Engineering","task":"continue the loop","async":false}`)
	if err == nil || !strings.Contains(err.Error(), "delegation cycle detected") {
		t.Fatalf("ExecuteAsync error = %v, want delegation cycle error", err)
	}
	if action != nil {
		t.Fatalf("ExecuteAsync action = %#v, want nil", action)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0 when cycle validation fails", got)
	}
}

func TestDelegateTool_ExecuteAsyncAlwaysAsyncRejectsDepth(t *testing.T) {
	var resolverCalls atomic.Int32
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		resolverCalls.Add(1)
		return nil, false, nil
	}
	dt := NewDelegateTool(
		"research",
		time.Minute,
		resolver,
		nil,
		nil,
		WorkDirInheritOnly,
		WithAlwaysAsyncDelegation(),
	)
	ctx := ContextWithDelegationChain(
		iface.ContextWithWorkDir(context.Background(), "/parent/project"),
		[]string{"planning", "engineering"},
	)

	action, err := dt.ExecuteAsync(ctx, `{"target":"research","task":"one more hop"}`)
	if err == nil || !strings.Contains(err.Error(), "delegation depth limit reached") {
		t.Fatalf("ExecuteAsync error = %v, want delegation depth error", err)
	}
	if action != nil {
		t.Fatalf("ExecuteAsync action = %#v, want nil", action)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0 when depth validation fails", got)
	}
}
