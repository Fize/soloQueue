package cron

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// TestRunTaskAttempts_AttemptFailedLogsRunIDStatusAndDiagnostic pins the
// diagnostic context of a failed attempt that returned no error: the reason
// lives in result.diagnosticError, so it must appear next to run_id in the
// same log entry to be correlateable with logs/cron/<task>/<run_id>/.
func TestRunTaskAttempts_AttemptFailedLogsRunIDStatusAndDiagnostic(t *testing.T) {
	logDir := t.TempDir()
	log, err := logger.System(logDir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	mgr := &mockSessionManager{}
	mgr.getSession = func(context.Context, string, string) (Session, bool, func(), error) {
		return &mockSession{
			idle: true,
			askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
				ch := make(chan iface.AgentEvent, 1)
				ch <- &testDoneEvent{content: "no structured submission"}
				close(ch)
				return ch, nil
			},
		}, true, func() {}, nil
	}

	s := NewScheduler(nil, mgr, log)
	s.retryDelay = 0
	s.SetWorkDir(t.TempDir())

	task := Task{ID: "task-1", Title: "title", TaskType: TaskTypeGeneral, Instruction: "do work"}
	var sess Session
	var cleanup func()
	if _, _, _ = s.runTaskAttempts(context.Background(), task, "run-1", &sess, &cleanup); cleanup != nil {
		cleanup()
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	path := filepath.Join(logDir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	var attempts int
	for _, entry := range readCronTestJSONL(t, path) {
		if entry["msg"] != "cron: task attempt failed" {
			continue
		}
		attempts++
		ctx, _ := entry["ctx"].(map[string]any)
		if ctx["run_id"] != "run-1" {
			t.Errorf("run_id = %v, want run-1", ctx["run_id"])
		}
		if ctx["status"] != "failed" {
			t.Errorf("status = %v, want failed", ctx["status"])
		}
		diag, _ := ctx["diagnostic"].(string)
		if !strings.Contains(diag, "missing_structured_submission") {
			t.Errorf("diagnostic = %q, want missing_structured_submission", diag)
		}
		if entry["err"] != nil {
			t.Errorf("err = %v, want null (attempt returned no error)", entry["err"])
		}
	}
	if attempts != maxCronRetries+1 {
		t.Fatalf("attempt failure log entries = %d, want %d", attempts, maxCronRetries+1)
	}
}

func readCronTestJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var entries []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			t.Fatalf("invalid JSONL: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return entries
}
