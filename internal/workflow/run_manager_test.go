package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

type runManagerTestExecutor struct{}

func (runManagerTestExecutor) Execute(context.Context, NodeRunRequest) (NodeRunResult, error) {
	return NodeRunResult{Handoff: &HandoffData{Outcome: "done", Content: "ok"}}, nil
}

type pausableRunExecutor struct {
	started chan struct{}
}

func (e pausableRunExecutor) Execute(ctx context.Context, _ NodeRunRequest) (NodeRunResult, error) {
	select {
	case e.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return NodeRunResult{}, ctx.Err()
}

func TestStartTaskPersistsWorktreeAndAudit(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rawWorkflow := []byte(`name: task-run
version: "1"
agents:
  worker:
    template: worker
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: do work
    outputs:
      done:
        to: []
`)
	wf, err := ParseWorkflow(rawWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	manager := newRunManagerWithStateRoot(NewEngine(runManagerTestExecutor{}, DefaultEngineLimits()), database, t.TempDir(), t.TempDir())
	task := WorkflowTask{Goal: "implement feature", AcceptanceCriteria: []string{"tests pass"}}
	id, err := manager.StartTask(context.Background(), wf, rawWorkflow, task, repo, "HEAD", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var detail *RunDetail
	for time.Now().Before(deadline) {
		detail, _ = manager.Get(id)
		if detail != nil && detail.Status == RunCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if detail == nil || detail.Status != RunCompleted {
		t.Fatalf("run did not complete: %+v", detail)
	}
	if detail.WorktreePath == "" || detail.WorktreePath == repo || detail.BranchName == "" {
		t.Fatalf("missing isolation metadata: %+v", detail.RunSummary)
	}
	if _, err := os.Stat(filepath.Join(detail.WorktreePath, ".git")); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	var eventCount int
	if err := database.QueryRow(`SELECT count(*) FROM workflow_run_events WHERE workflow_run_id = ?`, id).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount == 0 {
		t.Fatal("expected audit events")
	}
	var checkpointCount int
	if err := database.QueryRow(`SELECT count(*) FROM workflow_run_checkpoints WHERE workflow_run_id = ?`, id).Scan(&checkpointCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount == 0 {
		t.Fatal("expected checkpoint")
	}
	var worktreeCount int
	if err := database.QueryRow(`SELECT count(*) FROM workflow_worktrees WHERE workflow_run_id = ?`, id).Scan(&worktreeCount); err != nil {
		t.Fatal(err)
	}
	if worktreeCount != 1 {
		t.Fatalf("expected one worktree record, got %d", worktreeCount)
	}
	var persistedWorkDir, createdAt, updatedAt string
	if err := database.QueryRow(`SELECT work_dir, created_at, updated_at FROM workflow_runs WHERE id = ?`, id).Scan(&persistedWorkDir, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if persistedWorkDir != detail.RepositoryPath {
		t.Fatalf("work_dir = %q, want repository path %q", persistedWorkDir, detail.RepositoryPath)
	}
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		t.Fatalf("created_at is not RFC3339: %q", createdAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		t.Fatalf("updated_at is not RFC3339: %q", updatedAt)
	}
	if _, err := os.Stat(filepath.Join(repo, ".soloqueue")); !os.IsNotExist(err) {
		t.Fatalf("original repository was modified by manager: err=%v", err)
	}
}

func TestForcePauseRequiresExplicitResume(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rawWorkflow := []byte(`name: pause-run
version: "1"
agents:
  worker:
    template: worker
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: do work
    outputs:
      done:
        to: []
`)
	wf, err := ParseWorkflow(rawWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	manager := newRunManagerWithStateRoot(NewEngine(pausableRunExecutor{started: started}, DefaultEngineLimits()), database, t.TempDir(), t.TempDir())
	id, err := manager.StartTask(context.Background(), wf, rawWorkflow, WorkflowTask{Goal: "pause", AcceptanceCriteria: []string{"resume manually"}}, repo, "HEAD", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}
	if err := manager.Pause(id, "force"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ := manager.Get(id)
		if detail != nil && detail.Status == RunPaused {
			if !detail.ResumeAvailable {
				t.Fatal("paused run should advertise manual resume")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	detail, _ := manager.Get(id)
	t.Fatalf("run did not pause: %+v", detail)
}

func TestSchedulerSnapshotRoundTripPreservesResumeState(t *testing.T) {
	wf := mustParse(t, `
name: snapshot-state
version: "1"
agents:
  worker:
    template: dev
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: Start
    outputs: {done: {to: []}}
`)
	joinKey := JoinKey{NodeID: "merge", ActivationID: "activation-1"}
	loopKey := LoopKey{EdgeID: "review:retry:edit", ActivationID: "activation-1"}
	state := &RunState{
		ID:         "wf_snapshot",
		Workflow:   wf,
		Status:     RunPaused,
		NodeRuns:   map[string]*NodeRun{},
		ReadyQueue: []string{"queued:1"},
		JoinBuckets: map[JoinKey]*JoinBucket{
			joinKey: {
				Received: map[string]NodeInput{"left": {FromNode: "left", Outcome: "done", Content: "L", ActivationID: "activation-1"}},
				Expected: map[string]bool{"left": true, "right": true},
			},
		},
		LoopCounters: map[LoopKey]int{loopKey: 2},
		StartedAt:    time.Now(),
	}

	detail := snapshotRun(state)
	resume := buildResumeInput(&detail)
	if len(resume.ReadyQueue) != 1 || resume.ReadyQueue[0] != "queued:1" {
		t.Fatalf("ready queue = %v, want queued:1", resume.ReadyQueue)
	}
	if bucket := resume.JoinBuckets[joinKey]; bucket == nil || len(bucket.Received) != 1 || len(bucket.Expected) != 2 {
		t.Fatalf("join bucket was not preserved: %+v", bucket)
	}
	if resume.LoopCounters[loopKey] != 2 {
		t.Fatalf("loop counter = %d, want 2", resume.LoopCounters[loopKey])
	}
}

func TestPersistWritesSchedulerCheckpointColumns(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	detail := RunDetail{
		RunSummary:   RunSummary{ID: "wf_checkpoint", WorkflowName: "checkpoint", Status: RunPaused, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		ReadyQueue:   []string{"node:2"},
		JoinBuckets:  []JoinBucketView{{NodeID: "merge", ActivationID: "activation-1", Expected: []string{"left", "right"}}},
		LoopCounters: []LoopCounterView{{EdgeID: "review:retry:edit", ActivationID: "activation-1", Count: 2}},
	}
	manager.persist(detail)

	var readyJSON, joinJSON, loopJSON string
	if err := database.QueryRow(`SELECT ready_queue_json, join_buckets_json, loop_counters_json FROM workflow_run_checkpoints WHERE workflow_run_id = ? ORDER BY sequence DESC LIMIT 1`, detail.ID).Scan(&readyJSON, &joinJSON, &loopJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readyJSON, "node:2") || !strings.Contains(joinJSON, "merge") || !strings.Contains(loopJSON, "review:retry:edit") {
		t.Fatalf("checkpoint state missing: ready=%s join=%s loop=%s", readyJSON, joinJSON, loopJSON)
	}
}

func TestPersistSkipsIdenticalSnapshot(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	detail := RunDetail{RunSummary: RunSummary{ID: "wf_dedupe", WorkflowName: "dedupe", Status: RunRunning, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	manager.persist(detail)
	manager.persist(detail)

	var count int
	if err := database.QueryRow(`SELECT count(*) FROM workflow_run_checkpoints WHERE workflow_run_id = ?`, detail.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("checkpoint count = %d, want 1 for identical snapshots", count)
	}
}

func TestPersistBoundsRecoveryCheckpoints(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	detail := RunDetail{RunSummary: RunSummary{ID: "wf_retention", WorkflowName: "retention", Status: RunRunning, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	for i := 1; i <= 70; i++ {
		detail.CompletedCount = i
		manager.persist(detail)
	}

	var count, maxSequence int
	if err := database.QueryRow(`SELECT count(*), COALESCE(MAX(sequence), 0) FROM workflow_run_checkpoints WHERE workflow_run_id = ?`, detail.ID).Scan(&count, &maxSequence); err != nil {
		t.Fatal(err)
	}
	if count > 64 || maxSequence != 70 {
		t.Fatalf("checkpoint count=%d max_sequence=%d, want count<=64 and max_sequence=70", count, maxSequence)
	}
}

func TestPublishSkipsIdenticalSchedulerSnapshotAndAuditEvent(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	wf := mustParse(t, `
name: publish-dedupe
version: "1"
agents:
  worker: {template: dev}
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: Start
    outputs: {done: {to: []}}
`)
	state := &RunState{ID: "wf_publish_dedupe", Workflow: wf, Status: RunRunning, NodeRuns: map[string]*NodeRun{}, StartedAt: time.Now()}
	manager.publish(state)
	manager.publish(state)

	var checkpoints, events int
	if err := database.QueryRow(`SELECT count(*) FROM workflow_run_checkpoints WHERE workflow_run_id = ?`, state.ID).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM workflow_run_events WHERE workflow_run_id = ? AND event_type = 'state_snapshot'`, state.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 1 || events != 1 {
		t.Fatalf("checkpoints=%d state_events=%d, want 1/1", checkpoints, events)
	}
}

func TestReleaseControlDoesNotDeleteReplacement(t *testing.T) {
	manager := &RunManager{active: make(map[string]*runControl)}
	oldControl := &runControl{}
	replacement := &runControl{}
	manager.active["wf_control"] = replacement

	manager.releaseControl("wf_control", oldControl)

	if manager.active["wf_control"] != replacement {
		t.Fatal("old execution removed the replacement control")
	}
}

func TestClaimControlRejectsConcurrentExecution(t *testing.T) {
	manager := &RunManager{active: make(map[string]*runControl)}
	existing := &runControl{}
	manager.active["wf_control"] = existing

	err := manager.claimControl("wf_control", &runControl{})
	if err == nil || !strings.Contains(err.Error(), "workflow_resume_conflict") {
		t.Fatalf("claim error = %v, want resume conflict", err)
	}
	if manager.active["wf_control"] != existing {
		t.Fatal("concurrent claim replaced the existing control")
	}
}

func TestPauseRejectsTerminalRunBeforeControlRelease(t *testing.T) {
	manager := &RunManager{
		active: map[string]*runControl{"wf_terminal": {}},
		meta:   make(map[string]*runMetadata),
		runs: map[string]RunDetail{
			"wf_terminal": {RunSummary: RunSummary{ID: "wf_terminal", Status: RunCompleted}},
		},
	}

	err := manager.Pause("wf_terminal", "graceful")
	if err == nil || !strings.Contains(err.Error(), "workflow_pause_conflict") {
		t.Fatalf("Pause error = %v, want terminal status conflict", err)
	}
	if got := manager.runs["wf_terminal"].Status; got != RunCompleted {
		t.Fatalf("status = %s, want %s", got, RunCompleted)
	}
}

func TestDeliveryContextHasDeadlineAndInheritsCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := deliveryContext(parent)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("delivery context has no deadline")
	}
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("delivery context did not inherit run cancellation")
	}
}

func TestResolveConfirmationForwardsChoiceAndPersistsResult(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	choiceCh := make(chan string, 1)
	manager.recordConfirmation("wf_confirm", ConfirmationRequest{
		CallID:    "call-1",
		NodeRunID: "node-1",
		ToolName:  "shell",
		Resolve: func(choice string) error {
			choiceCh <- choice
			return nil
		},
	})

	if err := manager.ResolveConfirmation("wf_confirm", "call-1", "yes"); err != nil {
		t.Fatal(err)
	}
	select {
	case choice := <-choiceCh:
		if choice != "yes" {
			t.Fatalf("choice = %q, want yes", choice)
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation choice was not forwarded")
	}
	var status, choice string
	if err := database.QueryRow(`SELECT status, choice FROM workflow_confirmations WHERE call_id = ?`, "call-1").Scan(&status, &choice); err != nil {
		t.Fatal(err)
	}
	if status != "resolved" || choice != "yes" {
		t.Fatalf("status=%q choice=%q, want resolved/yes", status, choice)
	}
}

func TestReconcileLegacyRunDoesNotAdvertiseUnsupportedActions(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	legacy := RunDetail{RunSummary: RunSummary{ID: "wf_legacy", WorkflowName: "legacy", Status: RunRunning, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workflow_runs(id, workflow_name, status, started_at, snapshot_json) VALUES(?, ?, ?, ?, ?)`, legacy.ID, legacy.WorkflowName, legacy.Status, legacy.StartedAt, string(raw)); err != nil {
		t.Fatal(err)
	}

	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	detail, err := manager.Get(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != RunInterrupted || detail.ResumeAvailable || detail.RestartAvailable || detail.CleanupAvailable {
		t.Fatalf("legacy actions should be unavailable: %+v", detail.RunSummary)
	}
	if detail.ErrorCode != "workflow_legacy_metadata_missing" {
		t.Fatalf("error code = %q", detail.ErrorCode)
	}
}

func TestListOmitsWorkflowDefinition(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager := newRunManagerWithStateRoot(nil, database, t.TempDir(), t.TempDir())
	manager.persist(RunDetail{RunSummary: RunSummary{ID: "wf_list", WorkflowName: "list", Status: RunCompleted, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), WorkflowYAML: strings.Repeat("large-definition\n", 1000)}})

	runs, err := manager.List("list")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].WorkflowYAML != "" {
		t.Fatalf("list leaked workflow definition: %+v", runs)
	}
}

func initWorkflowTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitWorkflowTest(t, repo, "init", "-q")
	gitWorkflowTest(t, repo, "config", "user.email", "workflow-test@example.invalid")
	gitWorkflowTest(t, repo, "config", "user.name", "Workflow Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitWorkflowTest(t, repo, "add", "README.md")
	gitWorkflowTest(t, repo, "commit", "-qm", "base")
	return repo
}

func gitWorkflowTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
