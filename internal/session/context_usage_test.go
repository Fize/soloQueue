package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
)

func TestReadL2ContextUsageReplaysTimelineOnColdSession(t *testing.T) {
	workDir := t.TempDir()
	const id = "cold-session"
	tlDir := filepath.Join(workDir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := map[string]any{
		"ts": "2026-10-15T00:00:00Z", "type": "message",
		"msg": map[string]any{"role": "user", "content": "persisted conversation content that must restore token usage"},
	}
	data, _ := json.Marshal(event)
	if err := os.WriteFile(filepath.Join(tlDir, "timeline.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	builder := &Builder{WorkDir: workDir, RT: &runtime.Stack{Tokenizer: ctxwin.NewTokenizer()}}
	used, limit, err := builder.ReadL2ContextUsage(context.Background(), id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if used <= 0 || limit <= 0 {
		t.Fatalf("cold timeline usage = (%d,%d), want positive usage and limit", used, limit)
	}
}

func TestAdvanceUsageSegmentPersistsOnlySuccessfulBoundary(t *testing.T) {
	workDir := t.TempDir()
	const id = "segment-session"
	if err := SaveMeta(workDir, id, &SessionMeta{Group: "dev"}); err != nil {
		t.Fatal(err)
	}
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, "the context snapshot should be saved with the compact boundary")
	used, limit, _ := cw.TokenUsage()
	s := &Session{metaWorkDir: workDir, metaL2ID: id, usageMetaPath: MetaFilePath(workDir, id), cw: cw}
	s.advanceUsageSegment()
	meta, err := LoadMeta(workDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Usage.CurrentSegment != 1 || len(meta.Usage.Segments) != 2 || meta.CtxwinUsed != used || meta.CtxwinLimit != limit || meta.CtxwinUpdated.IsZero() {
		t.Fatalf("segment transition not persisted: %+v", meta.Usage)
	}
}

func TestSessionTelemetryUsagePersistsToMetaSegment(t *testing.T) {
	workDir := t.TempDir()
	const id = "telemetry-session"
	if err := SaveMeta(workDir, id, &SessionMeta{Group: "dev"}); err != nil {
		t.Fatal(err)
	}
	s := &Session{TargetID: "l2-" + id + "-session", metaWorkDir: workDir, metaL2ID: id, usageMetaPath: MetaFilePath(workDir, id)}
	metadata := telemetry.MetadataFromContext(s.withSessionTelemetry(context.Background()))
	if metadata.SegmentID != "0" {
		t.Fatalf("segment id = %q, want 0", metadata.SegmentID)
	}
	metadata.OnUsage(telemetryctx.Usage{
		InputTokens: 100, OutputTokens: 20, TotalTokens: 120,
		CacheHitTokens: 60, CacheMissTokens: 40, CacheDetailsReported: true,
	})
	got, err := LoadMeta(workDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.Lifetime.TotalTokens != 120 || got.Usage.Segments[0].Totals.CacheHitTokens != 60 {
		t.Fatalf("usage was not persisted to meta.json: %+v", got.Usage)
	}
}

func TestL1UsagePersistsBesideDefaultTimelineMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "timelines", "default", MetaFileName)
	s := &Session{TargetID: "l1-session", usageMetaPath: path, usageIndex: 0}
	metadata := telemetry.MetadataFromContext(s.withSessionTelemetry(context.Background()))
	metadata.OnUsage(telemetryctx.Usage{
		InputTokens: 80, OutputTokens: 20, TotalTokens: 100,
		CacheHitTokens: 50, CacheMissTokens: 30, CacheDetailsReported: true,
	})
	got, err := LoadMetaPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.Lifetime.TotalTokens != 100 || got.Usage.Segments[0].Totals.CacheHitTokens != 50 {
		t.Fatalf("L1 usage not restored from default timeline meta.json: %+v", got.Usage)
	}
}

type usageRecordingCompactor struct{}

func (usageRecordingCompactor) Compact(ctx context.Context, _ []ctxwin.Message) (string, error) {
	metadata := telemetry.MetadataFromContext(ctx)
	if metadata.SegmentID != "0" {
		return "", errors.New("compact call did not retain the segment being compacted")
	}
	metadata.OnUsage(telemetryctx.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CacheHitTokens: 6, CacheMissTokens: 4, CacheDetailsReported: true})
	return "compact summary", nil
}

func TestSuccessfulCompactCountsItsUsageInOldSegmentThenAdvances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "timelines", "default", MetaFileName)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer(), ctxwin.WithCompactor(usageRecordingCompactor{}))
	cw.Push(ctxwin.RoleSystem, "system")
	cw.Push(ctxwin.RoleUser, "history that should be compacted")
	cw.Push(ctxwin.RoleAssistant, "response that should be compacted")
	s := &Session{TargetID: "l1-session", usageMetaPath: path, usageIndex: 0, cw: cw, reflectionCtx: context.Background(), logger: newTestLog(t)}
	cw.SetLifecycleContext(s.usageTelemetryContext(0))
	cw.AddSummaryHook(func(context.Context, []ctxwin.SummarySegment, string) { s.advanceUsageSegment() })
	if _, err := s.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	meta, err := LoadMetaPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Usage.CurrentSegment != 1 || meta.Usage.Segments[0].Totals.TotalTokens != 12 || meta.Usage.Segments[1].Totals.TotalTokens != 0 {
		t.Fatalf("compact usage was not assigned to the closed segment: %+v", meta.Usage)
	}
}
