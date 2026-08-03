package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	workdirutil "github.com/xiaobaitu/soloqueue/internal/workdir"
)

func TestDelegateTool_PreferredTimeout_Explicit(t *testing.T) {
	dt := NewDelegateTool("leader", "desc", 20*time.Minute, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 20*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 20m", got)
	}
}

func TestDelegateTool_PreferredTimeout_Default(t *testing.T) {
	dt := NewDelegateTool("leader", "desc", 0, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != DelegateDefaultTimeout {
		t.Errorf("PreferredTimeout() = %v, want DelegateDefaultTimeout (%v)", got, DelegateDefaultTimeout)
	}
}

func TestDelegateTool_PreferredTimeout_Capped(t *testing.T) {
	// PreferredTimeout returns the raw dt.Timeout / DelegateDefaultTimeout;
	// the actual capping to DelegateMaxTimeout happens inside Execute/ExecuteAsync.
	dt := NewDelegateTool("leader", "desc", 99*time.Minute, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 99*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 99m (uncapped)", got)
	}
}

func TestDelegateTool_InheritOnlySchemaAndResolution(t *testing.T) {
	dt := NewDelegateTool("worker", "desc", time.Minute, nil, nil, WorkDirInheritOnly)
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
	dt := NewDelegateTool("leader", "desc", time.Minute, nil, nil, WorkDirExplicitOrInherited)
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
