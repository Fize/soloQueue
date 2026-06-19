package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// ─── Test fakes ──────────────────────────────────────────────────────────────

// fakeLocatable implements iface.Locatable for testing. It returns a canned
// content string from AskStream and tracks calls.
type fakeLocatable struct {
	name        string
	content     string
	err         error
	askStreamFn func(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	mu          sync.Mutex
	askCalls    []string
	stopped     bool
}

func (f *fakeLocatable) Ask(ctx context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.askCalls = append(f.askCalls, prompt)
	f.mu.Unlock()
	return f.content, f.err
}

func (f *fakeLocatable) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	f.mu.Lock()
	f.askCalls = append(f.askCalls, prompt)
	f.mu.Unlock()
	if f.askStreamFn != nil {
		return f.askStreamFn(ctx, prompt)
	}
	// Default: emit a DoneEvent-like event with the canned content.
	ch := make(chan iface.AgentEvent, 2)
	go func() {
		defer close(ch)
		if f.err != nil {
			ch <- fakeErrorEvent{err: f.err}
			return
		}
		ch <- fakeDoneEvent{content: f.content}
	}()
	return ch, nil
}

func (f *fakeLocatable) Confirm(callID string, choice string) error { return nil }
func (f *fakeLocatable) ErrorCount() int32                           { return 0 }
func (f *fakeLocatable) LastError() string                           { return "" }

func (f *fakeLocatable) Stop(timeout time.Duration) error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	return nil
}

func (f *fakeLocatable) InstanceID() string { return f.name }

// fakeDoneEvent and fakeErrorEvent implement iface.AgentEvent + EventConsumer.
type fakeDoneEvent struct{ content string }

func (fakeDoneEvent) IsAgentEvent() {}
func (e fakeDoneEvent) ContentDelta() (string, bool) { return "", false }
func (e fakeDoneEvent) DoneContent() (string, bool)  { return e.content, true }
func (fakeDoneEvent) Error() (error, bool)           { return nil, false }
func (fakeDoneEvent) ConfirmRequest() (string, bool) { return "", false }

type fakeErrorEvent struct{ err error }

func (fakeErrorEvent) IsAgentEvent()                  {}
func (fakeErrorEvent) ContentDelta() (string, bool)   { return "", false }
func (fakeErrorEvent) DoneContent() (string, bool)    { return "", false }
func (e fakeErrorEvent) Error() (error, bool)         { return e.err, true }
func (fakeErrorEvent) ConfirmRequest() (string, bool) { return "", false }

// fakeDoneNotifier wraps a fakeLocatable with OnDelegationDone tracking.
type fakeDoneNotifier struct {
	*fakeLocatable
	doneCalled bool
}

func (f *fakeDoneNotifier) OnDelegationDone() {
	f.mu.Lock()
	f.doneCalled = true
	f.mu.Unlock()
}

// ─── PeerTeamCatalog tests ───────────────────────────────────────────────────

func TestListPeerTeamsTool_ExcludesSelf(t *testing.T) {
	catalog := []PeerTeamInfo{
		{Name: "dev", Group: "engineering", LeaderDescription: "dev team", WorkerCount: 3},
		{Name: "ops", Group: "ops", LeaderDescription: "ops team", WorkerCount: 2},
	}
	tool := NewListPeerTeamsTool(catalog, "dev")

	args := `{}`
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if strings.Contains(result, `"dev"`) {
		t.Errorf("result should exclude self team 'dev', got: %s", result)
	}
	if !strings.Contains(result, `"ops"`) {
		t.Errorf("result should include peer team 'ops', got: %s", result)
	}
}

func TestListPeerTeamsTool_EmptyCatalog(t *testing.T) {
	tool := NewListPeerTeamsTool(nil, "dev")
	result, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "No peer teams available") {
		t.Errorf("expected 'No peer teams available' message, got: %s", result)
	}
}

func TestListPeerTeamsTool_NameAndSchema(t *testing.T) {
	tool := NewListPeerTeamsTool(nil, "dev")
	if tool.Name() != "list_peer_teams" {
		t.Errorf("Name() = %q, want 'list_peer_teams'", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Error("Parameters() should not be empty")
	}
}

// ─── RequestTeamHelpTool: cycle detection ────────────────────────────────────

func TestRequestTeamHelp_CycleDetection_DirectLoop(t *testing.T) {
	// L2a ("dev") tries to call back to itself → must be rejected.
	spawned := false
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		spawned = true
		return nil, false, errors.New("should not spawn")
	}
	tool := NewRequestTeamHelpTool(
		"dev",    // self team name
		spawnFn,  // locate-or-spawn
		nil,      // reap (not used when cycle detected)
		25*time.Minute,
	)

	// Chain: ["dev"] → requesting "dev" → cycle.
	ctx := ContextWithDelegationChain(context.Background(), []string{"dev"})
	args := `{"team_name":"dev","task":"help me","context":"need ops review"}`

	_, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
	if spawned {
		t.Error("spawnFn should not be called when cycle is detected")
	}
}

func TestRequestTeamHelp_CycleDetection_IndirectLoop(t *testing.T) {
	// Chain: dev → ops → dev → must be rejected at the "dev" step.
	spawned := false
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		spawned = true
		return &fakeLocatable{name: teamName, content: "ok"}, true, nil
	}
	tool := NewRequestTeamHelpTool("ops", spawnFn, nil, 25*time.Minute)

	// We are "ops", chain says we got here via "dev" → requesting "dev" = cycle.
	ctx := ContextWithDelegationChain(context.Background(), []string{"dev", "ops"})
	args := `{"team_name":"dev","task":"help","context":"ctx"}`

	_, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("expected cycle detection error for indirect loop, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
	if spawned {
		t.Error("spawnFn should not be called when cycle is detected")
	}
}

// ─── RequestTeamHelpTool: depth limit ────────────────────────────────────────

func TestRequestTeamHelp_DepthLimit(t *testing.T) {
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		return &fakeLocatable{name: teamName, content: "ok"}, true, nil
	}
	tool := NewRequestTeamHelpTool("current", spawnFn, nil, 25*time.Minute)

	// Chain already has maxPeerDepth entries → any new request must be rejected.
	chain := make([]string, maxPeerDepth)
	for i := range chain {
		chain[i] = "team-" + string(rune('a'+i))
	}
	ctx := ContextWithDelegationChain(context.Background(), chain)
	args := `{"team_name":"target","task":"help","context":"ctx"}`

	_, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("expected depth limit error, got nil")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Errorf("error should mention depth, got: %v", err)
	}
}

// ─── RequestTeamHelpTool: normal delegation with spawn+reap ──────────────────

func TestRequestTeamHelp_SpawnAndReap(t *testing.T) {
	var spawnedInstance *fakeLocatable
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		spawnedInstance = &fakeLocatable{name: teamName, content: "ops result: all good"}
		return spawnedInstance, true, nil
	}
	var reapedInstance iface.Locatable
	reapFn := func(loc iface.Locatable) {
		reapedInstance = loc
	}
	tool := NewRequestTeamHelpToolWithReap("dev", spawnFn, reapFn, 25*time.Minute)

	args := `{"team_name":"ops","task":"review my deployment","context":"deploying v2"}`
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result, "ops result: all good") {
		t.Errorf("result should contain target content, got: %s", result)
	}
	if spawnedInstance == nil {
		t.Fatal("spawnFn should have been called")
	}
	if reapedInstance == nil {
		t.Fatal("reapFn should have been called after delegation completes")
	}
	if reapedInstance != spawnedInstance {
		t.Error("reapFn should receive the spawned instance")
	}
}

// ─── RequestTeamHelpTool: chain propagation ──────────────────────────────────

func TestRequestTeamHelp_ChainPropagation(t *testing.T) {
	var capturedCtx context.Context
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		capturedCtx = ctx
		return &fakeLocatable{name: teamName, content: "ok"}, true, nil
	}
	reapFn := func(iface.Locatable) {}
	tool := NewRequestTeamHelpToolWithReap("dev", spawnFn, reapFn, 25*time.Minute)

	args := `{"team_name":"ops","task":"help","context":"ctx"}`
	_, _ = tool.Execute(context.Background(), args)

	// The spawned agent's ctx should carry a chain that includes "dev".
	chain := DelegationChainFromContext(capturedCtx)
	if len(chain) != 1 || chain[0] != "dev" {
		t.Errorf("chain should be ['dev'], got %v", chain)
	}
}

// ─── RequestTeamHelpTool: existing chain preserved + appended ────────────────

func TestRequestTeamHelp_ExistingChainAppended(t *testing.T) {
	var capturedCtx context.Context
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		capturedCtx = ctx
		return &fakeLocatable{name: teamName, content: "ok"}, true, nil
	}
	reapFn := func(iface.Locatable) {}
	tool := NewRequestTeamHelpToolWithReap("ops", spawnFn, reapFn, 25*time.Minute)

	// We are "ops", reached via "dev" → requesting "qa" → chain should be ["dev","ops"].
	ctx := ContextWithDelegationChain(context.Background(), []string{"dev"})
	args := `{"team_name":"qa","task":"help","context":"ctx"}`
	_, _ = tool.Execute(ctx, args)

	chain := DelegationChainFromContext(capturedCtx)
	want := []string{"dev", "ops"}
	if len(chain) != len(want) {
		t.Fatalf("chain len = %d, want %d", len(chain), len(want))
	}
	for i, v := range want {
		if chain[i] != v {
			t.Errorf("chain[%d] = %q, want %q", i, chain[i], v)
		}
	}
}

// ─── RequestTeamHelpTool: target error propagation ───────────────────────────

func TestRequestTeamHelp_TargetError(t *testing.T) {
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		return &fakeLocatable{name: teamName, err: errors.New("ops is down")}, true, nil
	}
	reapFn := func(iface.Locatable) {}
	tool := NewRequestTeamHelpToolWithReap("dev", spawnFn, reapFn, 25*time.Minute)

	args := `{"team_name":"ops","task":"help","context":"ctx"}`
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error from target, got nil")
	}
	if !strings.Contains(err.Error(), "ops is down") {
		t.Errorf("error should contain target error, got: %v", err)
	}
}

// ─── RequestTeamHelpTool: arg validation ─────────────────────────────────────

func TestRequestTeamHelp_EmptyTask(t *testing.T) {
	tool := NewRequestTeamHelpTool("dev", nil, nil, 25*time.Minute)
	_, err := tool.Execute(context.Background(), `{"team_name":"ops","task":"","context":""}`)
	if err == nil {
		t.Fatal("expected error for empty task, got nil")
	}
}

func TestRequestTeamHelp_EmptyTeamName(t *testing.T) {
	tool := NewRequestTeamHelpTool("dev", nil, nil, 25*time.Minute)
	_, err := tool.Execute(context.Background(), `{"team_name":"","task":"help","context":""}`)
	if err == nil {
		t.Fatal("expected error for empty team_name, got nil")
	}
}

func TestRequestTeamHelp_InvalidJSON(t *testing.T) {
	tool := NewRequestTeamHelpTool("dev", nil, nil, 25*time.Minute)
	_, err := tool.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ─── RequestTeamHelpTool: spawn failure ──────────────────────────────────────

func TestRequestTeamHelp_SpawnFailure(t *testing.T) {
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		return nil, false, errors.New("factory closed")
	}
	tool := NewRequestTeamHelpToolWithReap("dev", spawnFn, func(iface.Locatable) {}, 25*time.Minute)

	args := `{"team_name":"ops","task":"help","context":"ctx"}`
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error from spawn failure, got nil")
	}
	if !strings.Contains(err.Error(), "factory closed") {
		t.Errorf("error should contain spawn failure reason, got: %v", err)
	}
}

// ─── RequestTeamHelpTool: DoneNotifier triggers reap ─────────────────────────

func TestRequestTeamHelp_DoneNotifierTriggersReap(t *testing.T) {
	loc := &fakeLocatable{name: "ops", content: "done"}
	notifier := &fakeDoneNotifier{fakeLocatable: loc}
	spawnFn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
		return notifier, true, nil
	}
	tool := NewRequestTeamHelpToolWithReap("dev", spawnFn, func(iface.Locatable) {}, 25*time.Minute)

	args := `{"team_name":"ops","task":"help","context":"ctx"}`
	_, _ = tool.Execute(context.Background(), args)

	if !notifier.doneCalled {
		t.Error("OnDelegationDone should have been called on the target")
	}
}

// ─── RequestTeamHelpTool: name and schema ────────────────────────────────────

func TestRequestTeamHelp_NameAndSchema(t *testing.T) {
	tool := NewRequestTeamHelpTool("dev", nil, nil, 25*time.Minute)
	if tool.Name() != "request_team_help" {
		t.Errorf("Name() = %q, want 'request_team_help'", tool.Name())
	}
	params := tool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() is not valid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema should have 'properties' object")
	}
	requiredProps := []string{"team_name", "task", "context"}
	for _, p := range requiredProps {
		if _, ok := props[p]; !ok {
			t.Errorf("schema should have property %q", p)
		}
	}
}
