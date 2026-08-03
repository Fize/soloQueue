package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

func TestDelegateAgentTool_InheritOnlyWorkDir(t *testing.T) {
	tool := NewDelegateAgentTool(nil, nil, WorkDirInheritOnly)
	params := tool.Parameters()
	if !json.Valid(params) {
		t.Fatalf("invalid parameters schema: %s", params)
	}
	if strings.Contains(string(params), "work_dir") {
		t.Fatal("inherit-only delegate_agent schema must not expose work_dir")
	}

	ctx := iface.ContextWithWorkDir(context.Background(), "/parent/project")
	got, err := tool.resolveWorkDir(ctx, "/wrong/project")
	if err != nil {
		t.Fatalf("resolveWorkDir: %v", err)
	}
	if got != "/parent/project" {
		t.Fatalf("resolveWorkDir = %q, want parent directory", got)
	}
}

func TestDelegateAgentTool_ExplicitSchemaIsValid(t *testing.T) {
	tool := NewDelegateAgentTool(nil, nil, WorkDirExplicitOrInherited)
	params := tool.Parameters()
	if !json.Valid(params) {
		t.Fatalf("invalid parameters schema: %s", params)
	}
	if !strings.Contains(string(params), "work_dir") {
		t.Fatal("explicit delegate_agent schema should expose work_dir")
	}
}
