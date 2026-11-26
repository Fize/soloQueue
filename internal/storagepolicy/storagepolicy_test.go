package storagepolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root, rel string, size int, modified time.Time) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(full, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyOwnsEveryPath(t *testing.T) {
	cases := map[string]Category{
		"soloqueue.db": Database,
		"logs/observability/requests/abc/calls/x/y.json":        ExecutionDetails,
		"logs/observability/prompts/hash.json":                  ExecutionDetails,
		"logs/timelines/default/timeline-2026-11-26.jsonl":      SessionHistory,
		"logs/timelines/default/delegations/dlg_1/stream.jsonl": ExecutionDetails,
		"logs/cron/task/execution.json":                         CronHistory,
		"logs/cron/task/delegations/dlg_1/stream.jsonl":         CronHistory,
		"logs/system/app-2026-11-26.jsonl":                      RuntimeLogs,
		"logs/http/access-2026-11-26.jsonl":                     RuntimeLogs,
		"logs/stderr.log":                                       Legacy,
		"workspace/notes.md":                                    Other,
	}
	for rel, want := range cases {
		if got := Classify(rel); got != want {
			t.Errorf("Classify(%q) = %s, want %s", rel, got, want)
		}
	}
}

func TestScanCountsEveryCategory(t *testing.T) {
	root := t.TempDir()
	old := time.Now().AddDate(0, 0, -3)
	write(t, root, "logs/observability/requests/req1/meta.json", 10, old)
	write(t, root, "logs/observability/requests/req1/calls/g/c1.json", 100, old)
	write(t, root, "logs/observability/prompts/hash1.json", 1000, old)
	write(t, root, "logs/timelines/default/delegations/dlg_1/meta.json", 20, old)
	write(t, root, "logs/timelines/default/timeline-2026-11-20.jsonl", 50, old)
	write(t, root, "logs/cron/task/delegations/dlg_9/stream.jsonl", 70, old)

	l := Scan(root)
	usage := l.Usage()
	if usage[ExecutionDetails] != 10+100+20+1000 {
		t.Errorf("execution_details usage = %d, want %d", usage[ExecutionDetails], 1130)
	}
	if usage[CronHistory] != 70 {
		t.Errorf("cron_history usage = %d, want 70", usage[CronHistory])
	}
	if usage[SessionHistory] != 50 {
		t.Errorf("session_history usage = %d, want 50", usage[SessionHistory])
	}
	for _, u := range l.Units {
		if u.Key == "cron/task" && u.Category != CronHistory {
			t.Errorf("cron journal unit category = %s, want cron_history", u.Category)
		}
	}
}

// The pre-fix bug: cron journals were counted by the writer but not by the
// cleaner, so a store just under the limit refused every write. Usage is now
// per category and both sides read it from the same ledger.
func TestWriterAndCleanerAgreeOnTheSameTree(t *testing.T) {
	root := t.TempDir()
	old := time.Now().AddDate(0, 0, 2)
	write(t, root, "logs/observability/requests/req1/calls/g/c1.json", 900, old)
	write(t, root, "logs/cron/task/delegations/dlg_9/stream.jsonl", 100, old)

	cfg := DefaultPolicy()
	cfg.ExecutionDetails.MaxTotalBytes = MiB
	cfg.CronHistory.MaxTotalBytes = MiB

	l := Scan(root)
	usage := l.Usage()
	if usage[ExecutionDetails] != 900 {
		t.Fatalf("details usage = %d, want 900 (cron journal must not count)", usage[ExecutionDetails])
	}
	if Build(cfg, l, time.Now()).Empty() == false {
		t.Fatal("nothing is over its own limit, the plan must be empty")
	}

}

func TestBuildPrunesOldestDetailsToHeadroom(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for i, name := range []string{"a", "b", "c"} {
		write(t, root, "logs/observability/requests/"+name+"/calls/g/c.json", 400*1024, now.AddDate(0, 0, -3+i))
	}
	cfg := DefaultPolicy()
	cfg.ExecutionDetails.MaxTotalBytes = MiB
	l := Scan(root)
	p := Build(cfg, l, now)
	if p.Empty() {
		t.Fatal("over limit, expected a prune plan")
	}
	if p.Usage[ExecutionDetails] > LimitsFor(cfg, ExecutionDetails).Target() {
		t.Fatalf("projected usage %d must be at or below target %d", p.Usage[ExecutionDetails], LimitsFor(cfg, ExecutionDetails).Target())
	}
	if p.Delete[0].Key != "observability/requests/a" {
		t.Fatalf("oldest unit must go first, got %s", p.Delete[0].Key)
	}
}

func TestBuildKeepsActiveUnit(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	write(t, root, "logs/observability/requests/running/meta.json", 10, now)
	write(t, root, "logs/observability/requests/running/calls/g/c.json", 1200*1024, now)
	cfg := DefaultPolicy()
	cfg.ExecutionDetails.MaxTotalBytes = MiB
	p := Build(cfg, Scan(root), now)
	for _, u := range p.Delete {
		if u.Key == "observability/requests/running" {
			t.Fatal("the running request must never be pruned")
		}
	}
}

func TestApplyRemovesUnitsAndOrphanPrompts(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	oldHash := strings.Repeat("a", 64)
	keptHash := strings.Repeat("b", 64)
	writeJSON(t, root, "logs/observability/requests/old/meta.json", `{"status":"completed"}`)
	writeJSON(t, root, "logs/observability/requests/old/calls/g/c1.json", `{"hash":"`+oldHash+`"}`)
	writeJSON(t, root, "logs/observability/prompts/"+oldHash+".json", `{"messages":[]}`)
	writeJSON(t, root, "logs/observability/requests/new/meta.json", `{"status":"completed"}`)
	writeJSON(t, root, "logs/observability/requests/new/calls/g/c2.json", `{"hash":"`+keptHash+`"}`)
	writeJSON(t, root, "logs/observability/prompts/"+keptHash+".json", `{"messages":[]}`)
	aged := now.AddDate(0, 0, -5)
	for _, rel := range []string{
		"logs/observability/requests/old/meta.json",
		"logs/observability/requests/old/calls/g/c1.json",
		"logs/observability/prompts/" + oldHash + ".json",
	} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), aged, aged); err != nil {
			t.Fatal(err)
		}
	}

	cfg := DefaultPolicy()
	cfg.ExecutionDetails.RetentionDays = 1
	res, err := Apply(root, Build(cfg, Scan(root), now))
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedPrompt != 1 {
		t.Fatalf("deleted prompts = %d, want 1", res.DeletedPrompt)
	}
	if _, err := os.Stat(filepath.Join(root, "logs/observability/requests/old")); !os.IsNotExist(err) {
		t.Fatal("expired request unit must be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "logs/observability/prompts/"+keptHash+".json")); err != nil {
		t.Fatal("a referenced prompt must survive")
	}
}

func TestEnsureIsNoOpBelowLimit(t *testing.T) {
	root := t.TempDir()
	write(t, root, "logs/observability/requests/req/calls/g/c.json", 100, time.Now())
	res, err := Ensure(root, DefaultPolicy(), ExecutionDetails, 100)
	if err != nil || res.FreedBytes != 0 {
		t.Fatalf("unexpected prune below the limit: %+v %v", res, err)
	}
}

func TestFileLimitsUsesHTTPOverride(t *testing.T) {
	cfg := DefaultPolicy()
	cfg.RuntimeLogs.HTTPOverride = true
	cfg.RuntimeLogs.HTTPMaxFileBytes = 7 * MiB
	cfg.RuntimeLogs.HTTPRetentionDays = 3
	if got := FileLimits(cfg, "logs/http/access-2026-11-26.jsonl"); got.MaxFileBytes != 7*MiB || got.MaxDays != 3 {
		t.Fatalf("http limits = %+v", got)
	}
	if got := FileLimits(cfg, "logs/system/app-2026-11-26.jsonl"); got.MaxFileBytes != 50*MiB {
		t.Fatalf("system limits = %+v", got)
	}
}

// Capacity is the hard limit; the retention window only acts as a fallback so
// data that is already expired still gets dropped, but not while there is room.
func age(t *testing.T, root, rel string, at time.Time) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.Chtimes(full, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityFirstRetentionFallback(t *testing.T) {
	now := time.Now()
	cfg := DefaultPolicy()
	cfg.ExecutionDetails.MaxTotalBytes = 1 * MiB
	cfg.ExecutionDetails.RetentionDays = 2

	underCap := t.TempDir()
	writeJSON(t, underCap, "logs/observability/requests/a/meta.json", `{"status":"completed"}`)
	age(t, underCap, "logs/observability/requests/a/meta.json", now.AddDate(0, 0, -3))
	write(t, underCap, "logs/observability/requests/a/calls/g/c.json", 1024, now.AddDate(0, 0, -3))
	if plan := Build(cfg, Scan(underCap), now); !plan.Empty() {
		t.Fatalf("expired data must survive while the category is under its cap: %v", plan.Delete)
	}

	overCap := t.TempDir()
	for i, name := range []string{"old", "mid", "new"} {
		writeJSON(t, overCap, "logs/observability/requests/"+name+"/meta.json", `{"status":"completed"}`)
		age(t, overCap, "logs/observability/requests/"+name+"/meta.json", now.AddDate(0, 0, -1+i))
		write(t, overCap, "logs/observability/requests/"+name+"/calls/g/c.json", 400*1024, now.AddDate(0, 0, -1+i))
	}
	plan := Build(cfg, Scan(overCap), now)
	if plan.Empty() || plan.Delete[0].Key != "observability/requests/old" {
		t.Fatalf("over cap must drop the oldest unit first: %v", plan.Delete)
	}
	if plan.Usage[ExecutionDetails] > LimitsFor(cfg, ExecutionDetails).Target() {
		t.Fatalf("projected usage %d above target", plan.Usage[ExecutionDetails])
	}

	beyond := t.TempDir()
	writeJSON(t, beyond, "logs/observability/requests/stale/meta.json", `{"status":"completed"}`)
	age(t, beyond, "logs/observability/requests/stale/meta.json", now.AddDate(0, 0, -5))
	write(t, beyond, "logs/observability/requests/stale/calls/g/c.json", 1024, now.AddDate(0, 0, -5))
	if plan := Build(cfg, Scan(beyond), now); plan.Empty() {
		t.Fatal("nothing may outlive the retention window by more than the fallback factor")
	}
}
