package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

type mockSessionManager struct{}

func (mockSessionManager) Session() cron.Session {
	return nil
}

func TestHTTP_CronHandlers(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "entries.db")
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

	mux := NewMux(tempDir, nil, nil, WithToolsConfig(&toolsCfg))
	defer mux.Close()

	// 1. POST /api/cron - Create a task
	var taskID string
	{
		body := map[string]string{
			"expression":   "0 12 * * *",
			"instruction":  "Check daily logs",
			"target_agent": "L1",
		}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/cron", bytes.NewReader(data))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var created cron.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if created.Expression != "0 12 * * *" || created.Instruction != "Check daily logs" {
			t.Errorf("unexpected task fields: %+v", created)
		}
		taskID = created.ID
	}

	// 2. GET /api/cron - List tasks
	{
		req := httptest.NewRequest("GET", "/api/cron", nil)
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
		req := httptest.NewRequest("PUT", "/api/cron/"+taskID, bytes.NewReader(data))
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
		req := httptest.NewRequest("DELETE", "/api/cron/"+taskID, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	}

	// 5. GET /api/cron - Verify deletion
	{
		req := httptest.NewRequest("GET", "/api/cron", nil)
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
	mux := NewMux(tempDir, nil, nil)
	defer mux.Close()

	// Request when cron system is not configured
	req := httptest.NewRequest("GET", "/api/cron", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable, got %d", rec.Code)
	}
}
