package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

type workflowTestExecutor struct{}

func (workflowTestExecutor) Execute(context.Context, workflow.NodeRunRequest) (workflow.NodeRunResult, error) {
	return workflow.NodeRunResult{Handoff: &workflow.HandoffData{Outcome: "done", Content: "ok"}}, nil
}

const workflowTestYAML = `name: demo
description: test workflow
version: "1"
agents:
  worker:
    template: worker
entry:
  - start
nodes:
  - id: start
    agent: worker
    prompt: |
      Do work.
    outputs:
      done:
        to: []
`

func newWorkflowTestMux(t *testing.T) *Mux {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	db, err := db.Open(t.TempDir() + "/soloqueue.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := workflow.NewStore(t.TempDir(), 0)
	engine := workflow.NewEngine(workflowTestExecutor{}, workflow.DefaultEngineLimits())
	runs := workflow.NewRunManager(engine, db, t.TempDir())
	return NewMux(t.TempDir(), nil, WithWorkflow(store, runs))
}

func TestWorkflowDefinitionAPI(t *testing.T) {
	mux := newWorkflowTestMux(t)
	create := newLocalhostRequest(http.MethodPost, "/api/workflows/", strings.NewReader(`{"name":"demo","yaml":`+mustJSON(t, workflowTestYAML)+`}`))
	create.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}

	get := newLocalhostRequest(http.MethodGet, "/api/workflows/demo/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.YAML != workflowTestYAML {
		t.Fatalf("unexpected YAML round trip: %q", got.YAML)
	}

	validate := newLocalhostRequest(http.MethodPost, "/api/workflows/validate", strings.NewReader(`{"yaml":"name: nope"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, validate)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d", rec.Code)
	}
}

func TestWorkflowDraftCreationWithNameOnly(t *testing.T) {
	mux := newWorkflowTestMux(t)
	create := newLocalhostRequest(
		http.MethodPost,
		"/api/workflows/",
		strings.NewReader(`{"name":"draft-workflow"}`),
	)
	create.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"draft":true`) {
		t.Fatalf("create draft response does not identify a draft: %s", rec.Body.String())
	}

	get := newLocalhostRequest(http.MethodGet, "/api/workflows/draft-workflow/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("get draft status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"draft":true`) {
		t.Fatalf("get draft response does not identify a draft: %s", rec.Body.String())
	}

	update := newLocalhostRequest(http.MethodPut, "/api/workflows/draft-workflow/", strings.NewReader(`{"name":"draft-workflow","yaml":"name: draft-workflow\nversion: \"1\"\ndescription: saved draft\nagents: {}\nentry: []\nnodes: []\n"}`))
	update.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, update)
	if rec.Code != http.StatusOK {
		t.Fatalf("save draft status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"draft":true`) {
		t.Fatalf("save draft response does not identify a draft: %s", rec.Body.String())
	}

	run := newLocalhostRequest(http.MethodPost, "/api/workflows/draft-workflow/runs", strings.NewReader(`{"task":{"goal":"try draft","acceptance_criteria":["should not run"]}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, run)
	if rec.Code == http.StatusAccepted {
		t.Fatalf("draft workflow unexpectedly started: %s", rec.Body.String())
	}
}

func TestWorkflowDefinitionRejectsMissingAgentTemplate(t *testing.T) {
	mux := newWorkflowTestMux(t)
	mux.templates = []agent.AgentTemplate{{ID: "existing-agent"}}

	create := newLocalhostRequest(
		http.MethodPost,
		"/api/workflows/",
		strings.NewReader(`{"name":"demo","yaml":`+mustJSON(t, workflowTestYAML)+`}`),
	)
	create.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `missing template`) {
		t.Fatalf("unexpected error: %s", rec.Body.String())
	}
}

func TestWorkflowRunAPI(t *testing.T) {
	mux := newWorkflowTestMux(t)
	create := newLocalhostRequest(http.MethodPost, "/api/workflows/", strings.NewReader(`{"name":"demo","yaml":`+mustJSON(t, workflowTestYAML)+`}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}

	start := newLocalhostRequest(http.MethodPost, "/api/workflows/demo/runs", strings.NewReader(`{"input":"hello"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, start)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("legacy input status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "task.goal and task.acceptance_criteria are required") {
		t.Fatalf("unexpected error: %s", rec.Body.String())
	}
}

func TestWorkflowTaskRunAPIUsesIsolatedWorktree(t *testing.T) {
	repo := t.TempDir()
	gitWorkflowServerTest(t, repo, "init", "-q")
	gitWorkflowServerTest(t, repo, "config", "user.email", "workflow@example.invalid")
	gitWorkflowServerTest(t, repo, "config", "user.name", "Workflow")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitWorkflowServerTest(t, repo, "add", "README.md")
	gitWorkflowServerTest(t, repo, "commit", "-qm", "base")
	mux := newWorkflowTestMux(t)
	create := newLocalhostRequest(http.MethodPost, "/api/workflows/", strings.NewReader(`{"name":"demo","yaml":`+mustJSON(t, workflowTestYAML)+`}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	body := `{"task":{"goal":"implement feature","acceptance_criteria":["tests pass"]},"repository":` + mustJSON(t, repo) + `,"base_ref":"HEAD"}`
	start := newLocalhostRequest(http.MethodPost, "/api/workflows/demo/runs", strings.NewReader(body))
	start.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, start)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start status = %d: %s", rec.Code, rec.Body.String())
	}
	var started struct {
		ID string `json:"run_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		get := newLocalhostRequest(http.MethodGet, "/api/workflows/demo/runs/"+started.ID+"/", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, get)
		if strings.Contains(rec.Body.String(), `"completed"`) {
			if !strings.Contains(rec.Body.String(), `"task"`) || !strings.Contains(rec.Body.String(), `"worktree_path"`) {
				t.Fatalf("missing task/worktree metadata: %s", rec.Body.String())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task run did not complete")
}

func TestBuiltinWorkflowCatalogRequiresExplicitInstall(t *testing.T) {
	mux := newWorkflowTestMux(t)
	list := newLocalhostRequest(http.MethodGet, "/api/workflows/builtin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, list)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "engineering-quality-loop") {
		t.Fatalf("catalog status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(mux.workflowStore.Dir, "engineering-quality-loop.yaml")); !os.IsNotExist(err) {
		t.Fatalf("built-in workflow was installed implicitly: %v", err)
	}
	install := newLocalhostRequest(http.MethodPost, "/api/workflows/builtin/install", strings.NewReader(`{"names":["engineering-quality-loop"]}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, install)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"created":true`) {
		t.Fatalf("install status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowConfirmationResolveAPIRejectsUnknownPendingCall(t *testing.T) {
	mux := newWorkflowTestMux(t)
	if _, err := mux.workflowStore.Save("demo", []byte(workflowTestYAML)); err != nil {
		t.Fatal(err)
	}
	wf, err := mux.workflowStore.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := mux.workflowStore.ReadRaw("demo")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	gitWorkflowServerTest(t, repo, "init", "-q")
	gitWorkflowServerTest(t, repo, "config", "user.email", "workflow@example.invalid")
	gitWorkflowServerTest(t, repo, "config", "user.name", "Workflow")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitWorkflowServerTest(t, repo, "add", "README.md")
	gitWorkflowServerTest(t, repo, "commit", "-qm", "base")
	runID, err := mux.workflowRuns.StartTask(context.Background(), wf, raw, workflow.WorkflowTask{Goal: "test", AcceptanceCriteria: []string{"done"}}, repo, "HEAD", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	req := newLocalhostRequest(http.MethodPost, "/api/workflows/demo/runs/"+runID+"/confirmations/call-1/resolve", strings.NewReader(`{"choice":"yes"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("resolve status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workflow_confirmation_unavailable") {
		t.Fatalf("unexpected error: %s", rec.Body.String())
	}
}

func gitWorkflowServerTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
