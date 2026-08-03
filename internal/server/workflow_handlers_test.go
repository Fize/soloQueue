package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start status = %d: %s", rec.Code, rec.Body.String())
	}
	var started struct {
		ID string `json:"run_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		get := newLocalhostRequest(http.MethodGet, "/api/workflows/demo/runs/"+started.ID+"/", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, get)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), `"completed"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete: %d %s", rec.Code, rec.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
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
