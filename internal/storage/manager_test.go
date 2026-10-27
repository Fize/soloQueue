package storage

import (
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger/rotating"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestPreviewCleanupProtectsHistoryActiveAndSymlinks(t *testing.T) {
	root := t.TempDir()
	policy := config.DefaultStorageConfig()
	m := New(root, func() config.StorageConfig { return policy }, nil)
	defer m.Close()
	old := time.Now().AddDate(0, 0, -40).Format("2006-01-02")
	fixture(t, root, "logs/system/tool-"+old+".jsonl", "old")
	fixture(t, root, "logs/timelines/a/timeline-"+old+".jsonl", "history")
	fixture(t, root, "logs/stderr.log", "legacy")
	outside := t.TempDir()
	fixture(t, outside, "private", "keep")
	if err := os.Symlink(outside, filepath.Join(root, "logs", "system", "link")); err != nil {
		t.Fatal(err)
	}
	w, err := rotating.OpenDateSize(filepath.Join(root, "logs/system"), "active", 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	preview, err := m.Preview(policy, Revision(policy), false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != 1 {
		t.Fatalf("preview %#v", preview)
	}
	if _, err := m.Execute(preview.ID, preview.Revision, false); err != ErrStale {
		t.Fatalf("confirmation bypass %v", err)
	}
	res, err := m.Execute(preview.ID, preview.Revision, true)
	if err != nil || res.DeletedFiles != 1 {
		t.Fatalf("cleanup %#v %v", res, err)
	}
	for _, path := range []string{"logs/timelines/a/timeline-" + old + ".jsonl", "logs/stderr.log"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatal("protected file removed", path)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "private")); err != nil {
		t.Fatal("outside removed")
	}
	if _, err := m.Execute(preview.ID, preview.Revision, true); err != ErrStale {
		t.Fatal("reused preview")
	}
}
func TestChangedFileAndPolicyPreview(t *testing.T) {
	root := t.TempDir()
	p := config.DefaultStorageConfig()
	m := New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	fixture(t, root, "logs/stderr.log", "old")
	preview, err := m.Preview(p, Revision(p), true)
	if err != nil {
		t.Fatal(err)
	}
	fixture(t, root, "logs/stderr.log", "new modified body")
	res, err := m.Execute(preview.ID, preview.Revision, true)
	if err != nil || res.DeletedFiles != 0 || len(res.Errors) == 0 {
		t.Fatalf("changed file deleted %#v %v", res, err)
	}
	preview, err = m.Preview(p, Revision(p), true)
	if err != nil {
		t.Fatal(err)
	}
	p.RuntimeLogs.RetentionDays = 2
	if _, err := m.Execute(preview.ID, preview.Revision, true); err != ErrStale {
		t.Fatal("stale policy accepted")
	}
}
func TestUsageNoDoubleCountAndRestartStatus(t *testing.T) {
	root := t.TempDir()
	p := config.DefaultStorageConfig()
	fixture(t, root, "logs/stderr.log", "abcd")
	fixture(t, root, "soloqueue.db", "123456")
	m := New(root, func() config.StorageConfig { return p }, nil)
	u := m.Usage()
	if u.TotalBytes != 10 || u.FileBytes != 4 || u.DatabaseBytes != 6 {
		t.Fatalf("usage %#v", u)
	}
	preview, err := m.Preview(p, Revision(p), true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Execute(preview.ID, preview.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	m = New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	last, _ := m.Status()
	if last.DeletedFiles != res.DeletedFiles || last.FinishedAt.IsZero() {
		t.Fatalf("cleanup result not durable %#v", last)
	}
}
func TestTighteningAndScanFailure(t *testing.T) {
	p := config.DefaultStorageConfig()
	n := p
	n.ExecutionDetails.RetentionDays = 2
	if !Tightens(p, n) {
		t.Fatal("tightening missed")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "logs")); err != nil {
		t.Fatal(err)
	}
	m := New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	preview, err := m.Preview(p, Revision(p), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Errors) == 0 {
		t.Fatal("symlink root not reported")
	}
	if _, err := m.Execute(preview.ID, preview.Revision, true); err != ErrStale {
		t.Fatal("failed scan allowed cleanup")
	}
}

func TestCompletedDispatchCompactionPreservesRecovery(t *testing.T) {
	root := t.TempDir()
	p := config.DefaultStorageConfig()
	p.ExecutionDetails.MaxTotalMB = 1
	timelineRoot := filepath.Join(root, "logs/timelines/default")
	dm, err := dispatch.NewManager(timelineRoot, "l1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := dm.Begin(dispatch.BeginInput{Kind: dispatch.KindDelegate, TaskName: "large task", Task: "task", Requester: "leader", Executor: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Append(created.Record.ID, "tool_body", strings.Repeat("x", 2<<20)); err != nil {
		t.Fatal(err)
	}
	if err := dm.Finish(created.Record.ID, dispatch.StatusCompleted, nil); err != nil {
		t.Fatal(err)
	}
	m := New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	preview, err := m.Preview(p, Revision(p), false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != 1 {
		t.Fatalf("dispatch not selected %#v", preview)
	}
	res, err := m.Execute(preview.ID, preview.Revision, true)
	if err != nil || len(res.Errors) > 0 {
		t.Fatalf("compact %v %#v", err, res)
	}
	reloaded, err := dispatch.NewManager(timelineRoot, "l1")
	if err != nil {
		t.Fatal("recovery broken", err)
	}
	list := reloaded.List()
	if len(list) != 1 || list[0].Status != dispatch.StatusCompleted || !list[0].DetailsExpired {
		t.Fatalf("terminal summary lost %#v", list)
	}
}
func TestCronCleanupPreservesSummaryAndProtectsChangedFiles(t *testing.T) {
	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	p := config.DefaultStorageConfig()
	old := time.Now().AddDate(0, 0, -60).Format(time.RFC3339)
	dir := "logs/cron/task/execution"
	path := dir + "/timeline-2026-01-01.jsonl"
	fixture(t, root, path, "old trace")
	_, err = database.Exec(`INSERT INTO cron_execution_history (id,task_id,executed_at,completed_at,status,result_summary,timeline_dir) VALUES ('execution','task',?,?,'success','retained result',?)`, old, old, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(root, func() config.StorageConfig { return p }, database)
	defer m.Close()
	preview, err := m.Preview(p, Revision(p), false)
	if err != nil || preview.Files != 1 {
		t.Fatalf("preview %#v %v", preview, err)
	}
	fixture(t, root, path, "changed trace")
	res, err := m.Execute(preview.ID, preview.Revision, true)
	if err != nil || res.DeletedFiles != 0 {
		t.Fatalf("changed cron files deleted %#v %v", res, err)
	}
	var expired bool
	if err := database.QueryRow(`SELECT details_expired FROM cron_execution_history WHERE id='execution'`).Scan(&expired); err != nil || expired {
		t.Fatal("partial cleanup marked complete", err)
	}
	preview, err = m.Preview(p, Revision(p), false)
	if err != nil {
		t.Fatal(err)
	}
	res, err = m.Execute(preview.ID, preview.Revision, true)
	if err != nil || len(res.Errors) > 0 {
		t.Fatalf("cleanup %#v %v", res, err)
	}
	var summary, status, storedDir string
	if err := database.QueryRow(`SELECT result_summary,status,timeline_dir,details_expired FROM cron_execution_history WHERE id='execution'`).Scan(&summary, &status, &storedDir, &expired); err != nil {
		t.Fatal(err)
	}
	if summary != "retained result" || status != "success" || storedDir != "" || !expired {
		t.Fatalf("summary lost %q %q %q %v", summary, status, storedDir, expired)
	}
}

func TestInstalledPolicyBoundsCallAndPromptWrites(t *testing.T) {
	root := t.TempDir()
	p := config.DefaultStorageConfig()
	p.ExecutionDetails.MaxTotalMB = 4
	p.ExecutionDetails.MaxTurnMB = 4
	p.ExecutionDetails.MaxFileMB = 1
	m := New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	store := observability.ForWorkDir(root)
	now := time.Now()
	call := observability.Call{CallID: "large", SessionID: "l1", RequestID: "request", StartedAt: now, FinishedAt: &now, Status: "success", Content: strings.Repeat("body", 400000)}
	prompt := &observability.Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []observability.Instruction{{Role: "system", Content: strings.Repeat("prompt", 200000)}}}
	if err := store.Start("l1", "request"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCall(call, prompt); err != nil {
		t.Fatal(err)
	}
	request, err := store.Request("l1", "request")
	if err != nil || len(request.Calls) != 1 {
		t.Fatalf("request %#v %v", request, err)
	}
	got := request.Calls[0]
	if got.RecordingStatus != "partial" || got.Content != "" || got.PromptAvailable {
		t.Fatalf("per-file write policy bypassed: status=%s body=%d prompt=%v", got.RecordingStatus, len(got.Content), got.PromptAvailable)
	}
	// The same cheap Store constructor must observe live configuration, without local SetPolicy wiring.
	p.ExecutionDetails.MaxFileMB = 3
	call.CallID = "allowed"
	call.RequestID = "second"
	call.Content = strings.Repeat("ok", 300000)
	prompt.Messages[0].Content = "small actual system instruction"
	if err := store.SaveCall(call, prompt); err != nil {
		t.Fatal(err)
	}
	request, err = store.Request("l1", "second")
	if err != nil || len(request.Calls) != 1 || len(request.Calls[0].Content) != len(call.Content) || !request.Calls[0].PromptAvailable {
		t.Fatalf("live policy not applied %#v %v", request, err)
	}
}
func TestInstalledPolicySharesTurnBudgetWithDispatch(t *testing.T) {
	root := t.TempDir()
	p := config.DefaultStorageConfig()
	p.ExecutionDetails.MaxTotalMB = 4
	p.ExecutionDetails.MaxTurnMB = 1
	p.ExecutionDetails.MaxFileMB = 3
	m := New(root, func() config.StorageConfig { return p }, nil)
	defer m.Close()
	timelineRoot := filepath.Join(root, "logs", "timelines", "default")
	dm, err := dispatch.NewManager(timelineRoot, "l1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := dm.Begin(dispatch.BeginInput{TaskName: "shared budget", Task: "work", Requester: "leader", Executor: "worker", RequestID: "request"})
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Append(created.Record.ID, "tool_result", map[string]string{"content": strings.Repeat("d", 650000)}); err != nil {
		t.Fatal(err)
	}
	store := observability.ForWorkDir(root)
	now := time.Now()
	call := observability.Call{CallID: "same-turn-call", SessionID: "l1", RequestID: "request", StartedAt: now, FinishedAt: &now, Status: "success", Content: strings.Repeat("c", 650000)}
	if err := store.SaveCall(call, nil); err != nil {
		t.Fatal(err)
	}
	request, err := store.Request("l1", "request")
	if err != nil || len(request.Calls) != 1 {
		t.Fatalf("call missing %#v %v", request, err)
	}
	if request.Calls[0].RecordingStatus != "partial" || request.Calls[0].Content != "" {
		t.Fatal("dispatch body did not consume the same turn budget")
	}
	if err := dm.Append(created.Record.ID, "tool_result", map[string]string{"content": strings.Repeat("oversize", 300000)}); err != nil {
		t.Fatal(err)
	}
	if err := dm.Finish(created.Record.ID, dispatch.StatusCompleted, nil); err != nil {
		t.Fatal(err)
	}
	recovered, err := dispatch.NewManager(timelineRoot, "l1")
	if err != nil {
		t.Fatal("budget truncation corrupted recovery", err)
	}
	rec, ok := recovered.Get(created.Record.ID)
	if !ok || rec.Status != dispatch.StatusCompleted {
		t.Fatalf("recovery terminal lost %#v", rec)
	}
	events, err := dispatch.ReadEvents(filepath.Join(timelineRoot, "delegations", rec.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if len(event.Payload) > 1<<20 {
			t.Fatal("oversize dispatch payload bypassed shared budget")
		}
	}
	p.ExecutionDetails.MaxTotalMB = 1
	p.ExecutionDetails.MaxTurnMB = 4
	call.CallID = "other-request-call"
	call.RequestID = "other-request"
	if err := store.SaveCall(call, nil); err != nil {
		t.Fatal(err)
	}
	request, err = store.Request("l1", "other-request")
	if err != nil || len(request.Calls) != 1 || request.Calls[0].RecordingStatus != "partial" || request.Calls[0].Content != "" {
		t.Fatal("dispatch bytes bypassed the shared total budget", err)
	}
}

func TestCronCompletedCountBoundaryAndProtection(t *testing.T) {
	for _, tc := range []struct {
		n       int
		protect bool
		want    int
	}{{2, false, 0}, {3, false, 1}, {4, true, 1}} {
		t.Run(fmt.Sprintf("%d-%v", tc.n, tc.protect), func(t *testing.T) {
			root := t.TempDir()
			database, err := db.Open(filepath.Join(root, "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			p := config.DefaultStorageConfig()
			p.CronHistory.MaxFiles = 2
			p.CronHistory.MaxPerTask = 0
			p.CronHistory.RetentionDays = 0
			p.CronHistory.MaxTotalMB = 0
			for i := 0; i < tc.n; i++ {
				id := fmt.Sprintf("run-%d", i)
				dir := "logs/cron/task/" + id
				fixture(t, root, dir+"/timeline-2026-01-01.jsonl", "trace")
				when := time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339)
				_, err = database.Exec(`INSERT INTO cron_execution_history (id,task_id,executed_at,completed_at,status,timeline_dir) VALUES (?, 'task', ?, ?, 'success', ?)`, id, when, when, dir)
				if err != nil {
					t.Fatal(err)
				}
				if tc.protect && i == 0 {
					fixture(t, root, dir+"/delegations/dlg_active/meta.json", `{"status":"running"}`)
				}
			}
			m := New(root, func() config.StorageConfig { return p }, database)
			defer m.Close()
			preview, err := m.Preview(p, Revision(p), false)
			if err != nil {
				t.Fatal(err)
			}
			if preview.Files != tc.want {
				t.Fatalf("selected files %d want %d", preview.Files, tc.want)
			}
		})
	}
}
