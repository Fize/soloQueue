package ctxwin

import (
	"testing"
)

func TestHasToolResult_Match(t *testing.T) {
	cw := newTestCW(100000, 10000)
	cw.Push(RoleAssistant, "", WithToolCalls(nil))
	cw.Push(RoleTool, "rendered skill content v1", WithToolName("Skill"), WithToolCallID("call-1"))
	cw.Push(RoleTool, "other tool output", WithToolName("Read"), WithToolCallID("call-2"))

	if !cw.HasToolResult("Skill", "rendered skill content v1") {
		t.Error("identical Skill content should be detected as loaded")
	}
}

func TestHasToolResult_DifferentContent(t *testing.T) {
	cw := newTestCW(100000, 10000)
	cw.Push(RoleTool, "rendered skill content v1", WithToolName("Skill"), WithToolCallID("call-1"))

	if cw.HasToolResult("Skill", "rendered skill content v2") {
		t.Error("different args rendering should not be treated as loaded")
	}
}

func TestHasToolResult_DifferentTool(t *testing.T) {
	cw := newTestCW(100000, 10000)
	cw.Push(RoleTool, "same text", WithToolName("Read"), WithToolCallID("call-1"))

	if cw.HasToolResult("Skill", "same text") {
		t.Error("content from another tool should not count as a loaded skill")
	}
}

func TestHasToolResult_EmptyWindow(t *testing.T) {
	cw := newTestCW(100000, 10000)
	if cw.HasToolResult("Skill", "anything") {
		t.Error("empty window should report not loaded")
	}
}
