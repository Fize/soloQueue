package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

type mockSessionManager struct{}

func (mockSessionManager) Session() cron.Session {
	return nil
}

func (mockSessionManager) GetSession(ctx context.Context, teamID, taskID string) (cron.Session, bool, func(), error) {
	return nil, false, nil, nil
}

func TestHTTP_CronHandlers(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "soloqueue.db")
	sdb, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite DB: %v", err)
	}
	defer sdb.Close()

	store := cron.NewDBStore(sdb)
	sched := cron.NewScheduler(store, mockSessionManager{}, nil)
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("failed to start scheduler: %v", err)
	}
	defer sched.Stop()

	toolsCfg := tools.Config{
		CronStore:     store,
		CronScheduler: sched,
	}

	mux := NewMux(tempDir, nil, WithToolsConfig(&toolsCfg))
	defer mux.Close()

	// Required title and task level cannot be omitted or invalid.
	for name, body := range map[string]map[string]string{
		"missing metadata": {"expression": "daily", "instruction": "Check logs"},
		"invalid level":    {"title": "Check logs", "task_level": "L9", "expression": "daily", "instruction": "Check logs"},
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(body)
			req := newLocalhostRequest("POST", "/api/cron", bytes.NewReader(data))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// 1. POST /api/cron - Create a task
	var taskID string
	{
		body := map[string]string{
			"title":        "Daily log check",
			"task_level":   "L1",
			"expression":   "0 12 * * *",
			"instruction":  "Check daily logs",
			"target_agent": "L1",
		}
		data, _ := json.Marshal(body)
		req := newLocalhostRequest("POST", "/api/cron", bytes.NewReader(data))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var created cron.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if created.Title != "Daily log check" || created.TaskLevel != "L1" || created.Expression != "0 12 * * *" || created.Instruction != "Check daily logs" {
			t.Errorf("unexpected task fields: %+v", created)
		}
		taskID = created.ID
	}

	// 2. GET /api/cron - List tasks
	{
		req := newLocalhostRequest("GET", "/api/cron", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var tasks []cron.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
			t.Fatalf("failed to parse list response: %v", err)
		}
		if len(tasks) != 1 || tasks[0].ID != taskID {
			t.Errorf("expected 1 task with ID %s, got %+v", taskID, tasks)
		}
	}

	// 3. PUT /api/cron/{id} - Update task expression and status to paused
	{
		body := map[string]string{
			"expression":  "0 15 * * *",
			"status":      "paused",
			"instruction": "Check afternoon logs",
		}
		data, _ := json.Marshal(body)
		req := newLocalhostRequest("PUT", "/api/cron/"+taskID, bytes.NewReader(data))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var updated cron.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
			t.Fatalf("failed to parse update response: %v", err)
		}
		if updated.Expression != "0 15 * * *" || updated.Status != "paused" || updated.Instruction != "Check afternoon logs" {
			t.Errorf("unexpected updated fields: %+v", updated)
		}
	}

	// 4. DELETE /api/cron/{id} - Delete task
	{
		req := newLocalhostRequest("DELETE", "/api/cron/"+taskID, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	}

	// 5. GET /api/cron - Verify deletion
	{
		req := newLocalhostRequest("GET", "/api/cron", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var tasks []cron.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
			t.Fatalf("failed to parse list response: %v", err)
		}
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %+v", tasks)
		}
	}
}

func TestHTTP_CronHandlers_Invalid(t *testing.T) {
	tempDir := t.TempDir()
	mux := NewMux(tempDir, nil)
	defer mux.Close()

	// Request when cron system is not configured
	req := newLocalhostRequest("GET", "/api/cron", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable, got %d", rec.Code)
	}
}

func TestHTTP_CronHistory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "soloqueue.db")
	sdb, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite DB: %v", err)
	}
	defer sdb.Close()

	store := cron.NewDBStore(sdb)
	sched := cron.NewScheduler(store, mockSessionManager{}, nil)
	sched.SetWorkDir(tempDir)
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("failed to start scheduler: %v", err)
	}
	defer sched.Stop()

	toolsCfg := tools.Config{
		CronStore:     store,
		CronScheduler: sched,
	}

	mux := NewMux(tempDir, nil, WithToolsConfig(&toolsCfg))
	defer mux.Close()

	ctx := context.Background()
	now := time.Now()

	// Insert execution records for a task.
	rec1 := cron.ExecutionRecord{
		ID: "exec-1", TaskID: "task-1",
		ExecutedAt: now.Add(-2 * time.Hour), CompletedAt: now.Add(-2*time.Hour).Add(time.Second),
		DurationMs: 1000, Status: "success", ResultSummary: "all good",
		TaskLevel: "L1", TargetAgent: "L1", ModelID: "m1", ProviderID: "p1",
		TimelineDir: "logs/cron/task-1/exec-1",
	}
	store.RecordExecution(ctx, rec1)

	rec2 := cron.ExecutionRecord{
		ID: "exec-2", TaskID: "task-1",
		ExecutedAt: now.Add(-1 * time.Hour), CompletedAt: now.Add(-1*time.Hour).Add(2*time.Second),
		DurationMs: 2000, Status: "failed", ErrorMessage: "timeout",
		TaskLevel: "L1", TargetAgent: "L1",
		TimelineDir: "logs/cron/task-1/exec-2",
	}
	store.RecordExecution(ctx, rec2)

	// GET /api/cron/task-1/history - list records.
	{
		req := newLocalhostRequest("GET", "/api/cron/task-1/history", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var records []cron.ExecutionRecord
		if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
		// Newest first.
		if records[0].ID != "exec-2" || records[1].ID != "exec-1" {
			t.Errorf("unexpected order: %+v", records)
		}
	}

	// GET /api/cron/task-1/history with limit.
	{
		req := newLocalhostRequest("GET", "/api/cron/task-1/history?limit=1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var records []cron.ExecutionRecord
		json.Unmarshal(rec.Body.Bytes(), &records)
		if len(records) != 1 {
			t.Errorf("expected 1 record with limit, got %d", len(records))
		}
		if records[0].ID != "exec-2" {
			t.Errorf("expected exec-2, got %s", records[0].ID)
		}
	}

	// GET /api/cron/task-1/history/exec-1 - detail (timeline may not exist, but metadata should).
	{
		req := newLocalhostRequest("GET", "/api/cron/task-1/history/exec-1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Execution cron.ExecutionRecord `json:"execution"`
			Events    []interface{}        `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse detail: %v", err)
		}
		if resp.Execution.ID != "exec-1" {
			t.Errorf("expected exec-1, got %s", resp.Execution.ID)
		}
	}

	// GET /api/cron/task-1/history/nonexistent - 404.
	{
		req := newLocalhostRequest("GET", "/api/cron/task-1/history/nonexistent", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	}

	// GET /api/cron/task-1/history with offset.
	{
		req := newLocalhostRequest("GET", "/api/cron/task-1/history?offset=1&limit=1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var records []cron.ExecutionRecord
		json.Unmarshal(rec.Body.Bytes(), &records)
		if len(records) != 1 || records[0].ID != "exec-1" {
			t.Errorf("unexpected offset result: %+v", records)
		}
	}
}
