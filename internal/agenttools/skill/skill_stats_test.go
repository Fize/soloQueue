package skill

import (
	"context"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func newTestStats(t *testing.T) *SQLiteInvocationStats {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewSQLiteInvocationStats(d)
}

func TestSQLiteInvocationStats_RecordAndCounts(t *testing.T) {
	ctx := context.Background()
	stats := newTestStats(t)
	now := time.Now()

	if err := stats.Record(ctx, InvocationEvent{AgentID: "l1", SkillID: "docx", Result: "ok", Duration: 100 * time.Millisecond}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := stats.Record(ctx, InvocationEvent{AgentID: "editor", SkillID: "docx", Result: "ok"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := stats.Record(ctx, InvocationEvent{AgentID: "l1", SkillID: "pdf", Result: "not_found"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	counts, err := stats.Counts(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts["docx"] != 2 {
		t.Errorf("docx count = %d, want 2 (aggregated across agents)", counts["docx"])
	}
	if counts["pdf"] != 0 {
		t.Errorf("pdf count = %d, want 0 (not_found is not usage)", counts["pdf"])
	}
}

func TestSQLiteInvocationStats_CountsFiltersBySince(t *testing.T) {
	ctx := context.Background()
	stats := newTestStats(t)

	if _, err := stats.db.Exec(`INSERT INTO skill_invocations (skill_id, result, created_at) VALUES ('old-skill', 'ok', '2024-01-01 00:00:00')`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if err := stats.Record(ctx, InvocationEvent{SkillID: "fresh", Result: "ok"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	counts, err := stats.Counts(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if _, ok := counts["old-skill"]; ok {
		t.Errorf("rows older than since should be excluded: %v", counts)
	}
	if counts["fresh"] != 1 {
		t.Errorf("fresh count = %d, want 1", counts["fresh"])
	}
}

func TestSQLiteInvocationStats_CountsEmpty(t *testing.T) {
	stats := newTestStats(t)
	counts, err := stats.Counts(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("empty table should yield empty counts: %v", counts)
	}
}

func TestSQLiteInvocationStats_RecordsDuration(t *testing.T) {
	ctx := context.Background()
	stats := newTestStats(t)

	if err := stats.Record(ctx, InvocationEvent{SkillID: "slow", Result: "ok", Duration: 2500 * time.Millisecond}); err != nil {
		t.Fatalf("record: %v", err)
	}
	var durationMs int64
	if err := stats.db.QueryRow(`SELECT duration_ms FROM skill_invocations WHERE skill_id = 'slow'`).Scan(&durationMs); err != nil {
		t.Fatalf("query duration: %v", err)
	}
	if durationMs != 2500 {
		t.Errorf("duration_ms = %d, want 2500", durationMs)
	}
}

func TestSQLiteInvocationStats_CountsExcludesFailures(t *testing.T) {
	ctx := context.Background()
	stats := newTestStats(t)

	for _, r := range []InvocationResult{InvocationOK, InvocationNotFound, InvocationFork, InvocationError} {
		if err := stats.Record(ctx, InvocationEvent{SkillID: "mixed", Result: r}); err != nil {
			t.Fatalf("record %s: %v", r, err)
		}
	}
	counts, err := stats.Counts(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	// Only successful invocations (ok/fork) count as usage; not_found and
	// error must not inflate ordering or governance's never-invoked list.
	if counts["mixed"] != 2 {
		t.Errorf("only ok+fork should count: got %d, want 2", counts["mixed"])
	}
}
