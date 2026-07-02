package logger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Layer / Category ────────────────────────────────────────────────────────

func TestValidCategory(t *testing.T) {
	tests := []struct {
		cat  Category
		want bool
	}{
		{CatApp, true},
		{CatConfig, true},
		{CatHTTP, true},
		{CatWS, true},
		{CatLLM, true},
		{CatTeam, true},
		{CatAgent, true},
		{CatActor, true},
		{CatTool, true},
		{CatMessages, true},
		{Category("bogus"), false},
		{Category(""), false},
	}
	for _, tt := range tests {
		got := ValidCategory(tt.cat)
		if got != tt.want {
			t.Errorf("ValidCategory(%q) = %v, want %v", tt.cat, got, tt.want)
		}
	}
}

func TestLayerForCategory_Removed(t *testing.T) {
	// LayerForCategory has been removed; all categories are now in the system layer.
	// Verify ValidCategory works for all known categories.
	for _, cat := range systemCategories {
		if !ValidCategory(cat) {
			t.Errorf("ValidCategory(%q) = false, want true", cat)
		}
	}
}

// ─── Basic Write Paths ───────────────────────────────────────────────────────

func TestSystemLogger_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Info(CatApp, "test message", "key", "value")
	log.Warn(CatConfig, "config changed")
	log.Error(CatHTTP, "request failed", "status", 500)

	time.Sleep(20 * time.Millisecond)
	if err := log.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	checkJSONLFile(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"), 1)
	checkJSONLFile(t, filepath.Join(dir, "logs", "system", "config-"+today()+".jsonl"), 1)
	checkJSONLFile(t, filepath.Join(dir, "logs", "system", "http-"+today()+".jsonl"), 1)
}

func TestTeamLogger_NowSystemPath(t *testing.T) {
	// Team logger has been removed; all logs go to system directory.
	// Verify team/agent categories write to system directory.
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Info(CatTeam, "team created", "memberCount", 3)
	log.Info(CatAgent, "agent spawned", "name", "leader")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	// Verify the directory is logs/system/
	teamFile := filepath.Join(dir, "logs", "system", "team-"+today()+".jsonl")
	checkJSONLFile(t, teamFile, 1)

	agentFile := filepath.Join(dir, "logs", "system", "agent-"+today()+".jsonl")
	checkJSONLFile(t, agentFile, 1)

	// Verify the layer field no longer appears
	entry := readFirstEntry(t, teamFile)
	if _, has := entry["layer"]; has {
		t.Errorf("layer field should not appear, got: %v", entry["layer"])
	}
}

func TestSessionLogger_NowSystemPath(t *testing.T) {
	// Session logger has been removed; all logs go to system directory.
	// Verify session-specific categories write to system directory.
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Info(CatLLM, "llm call", "model", "deepseek-chat")
	log.Debug(CatActor, "actor message")
	time.Sleep(20 * time.Millisecond)

	llmFile := filepath.Join(dir, "logs", "system", "llm-"+today()+".jsonl")
	checkJSONLFile(t, llmFile, 1)
	entry := readFirstEntry(t, llmFile)
	// session_id/team_id should not appear by default (only if explicitly passed as attrs)
	if _, has := entry["session_id"]; has {
		t.Errorf("session_id should not appear by default, got: %v", entry["session_id"])
	}
	if _, has := entry["team_id"]; has {
		t.Errorf("team_id should not appear by default, got: %v", entry["team_id"])
	}

	_ = log.Close()
}

// ─── Level Filtering ─────────────────────────────────────────────────────────

func TestLogger_LevelFilter_Warn(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithLevel(slog.LevelWarn))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Debug(CatApp, "debug message")
	log.Info(CatApp, "info message")
	log.Warn(CatApp, "warn message")
	log.Error(CatApp, "error message")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	entries := readAllEntries(t, appFile)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (Warn+Error), got %d", len(entries))
	}
	levels := []string{entries[0]["level"].(string), entries[1]["level"].(string)}
	if levels[0] != "WARN" || levels[1] != "ERROR" {
		t.Errorf("levels = %v, want [WARN ERROR]", levels)
	}
}

func TestLogger_LevelFilter_Debug_AllThrough(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Debug(CatApp, "d")
	log.Info(CatApp, "i")
	log.Warn(CatApp, "w")
	log.Error(CatApp, "e")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entries := readAllEntries(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if len(entries) != 4 {
		t.Errorf("expected 4 entries at Debug level, got %d", len(entries))
	}
}

func TestLogger_LevelFilter_Error_OnlyError(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithLevel(slog.LevelError))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Debug(CatApp, "d")
	log.Info(CatApp, "i")
	log.Warn(CatApp, "w")
	log.Error(CatApp, "e")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	entries := readAllEntries(t, appFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (Error only), got %d", len(entries))
	}
	if entries[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entries[0]["level"])
	}
}

// ─── Invalid Category Fallback ───────────────────────────────────────────────

func TestLogger_InvalidCategory_FallbackAndNoPanic(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	// Unknown category should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logging invalid category panicked: %v", r)
		}
	}()

	log.Info(Category("bogus"), "unknown category")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	// No fallback: logs are written to the corresponding file based on the original category
	bogusFile := filepath.Join(dir, "logs", "system", "bogus-"+today()+".jsonl")
	if _, err := os.Stat(bogusFile); err != nil {
		t.Errorf("expected log in bogus-*.jsonl (category used as-is), stat err: %v", err)
	}
}

func TestLogger_EmptyCategory_Fallback(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	log.Info(Category(""), "empty category")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	// Should fallback to app (the first category in the system layer)
	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	if _, err := os.Stat(appFile); err != nil {
		t.Errorf("empty category should fallback to app; stat err: %v", err)
	}
}

// ─── Child Logger / Attrs ────────────────────────────────────────────────────

func TestChildLogger_InheritsTraceID(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	child := log.WithTraceID("abc123")
	child.Info(CatApp, "child message")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["trace_id"] != "abc123" {
		t.Errorf("trace_id = %v, want abc123", entry["trace_id"])
	}
}

func TestChildLogger_NestedInheritance(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	// Nested child
	c1 := log.Child(slog.String("actor_id", "worker-1"))
	c2 := c1.WithTraceID("trace-xyz")
	c2.Info(CatApp, "nested child")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["actor_id"] != "worker-1" {
		t.Errorf("actor_id = %v, want worker-1", entry["actor_id"])
	}
	if entry["trace_id"] != "trace-xyz" {
		t.Errorf("trace_id = %v, want trace-xyz", entry["trace_id"])
	}
}

func TestNewTraceID_RandomHex(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		c := log.NewTraceID()
		c.Info(CatApp, "test")
	}
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entries := readAllEntries(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if len(entries) != 5 {
		t.Fatalf("want 5 entries, got %d", len(entries))
	}
	for _, e := range entries {
		tid, ok := e["trace_id"].(string)
		if !ok || len(tid) != 8 {
			t.Errorf("trace_id should be 8 hex chars, got %v", e["trace_id"])
		}
		if seen[tid] {
			t.Errorf("duplicate trace_id %q across calls — RNG not working", tid)
		}
		seen[tid] = true
	}
}

// ─── Top-level vs ctx Field Placement ────────────────────────────────────────

func TestLogger_FieldPlacement(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	log.Info(CatApp, "placement test",
		slog.String("trace_id", "t1"),
		slog.String("actor_id", "a1"),
		slog.Int64("duration_ms", 42),
		slog.String("custom_field", "xyz"),
		slog.Int("count", 7),
	)
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))

	// Top-level reserved fields
	if entry["trace_id"] != "t1" {
		t.Errorf("trace_id not at top level: %v", entry)
	}
	if entry["actor_id"] != "a1" {
		t.Errorf("actor_id not at top level: %v", entry)
	}
	// duration_ms top level (numbers might deserialize to float64)
	if v, ok := entry["duration_ms"].(float64); !ok || v != 42 {
		t.Errorf("duration_ms not at top level or wrong value: %v", entry["duration_ms"])
	}

	// Custom fields go into ctx
	ctx, ok := entry["ctx"].(map[string]any)
	if !ok {
		t.Fatalf("ctx field missing or wrong type: %v", entry["ctx"])
	}
	if ctx["custom_field"] != "xyz" {
		t.Errorf("custom_field not in ctx: %v", ctx)
	}
	if v, ok := ctx["count"].(float64); !ok || v != 7 {
		t.Errorf("count not in ctx or wrong: %v", ctx["count"])
	}

	// Custom fields should not appear at the top level
	if _, exists := entry["custom_field"]; exists {
		t.Errorf("custom_field should not be at top level")
	}
}

func TestLogger_NoCustomFields_NoCtxKey(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	log.Info(CatApp, "no custom fields", slog.String("trace_id", "t1"))
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if _, has := entry["ctx"]; has {
		t.Errorf("ctx should be absent when no custom fields, got: %v", entry["ctx"])
	}
}

// ─── LogError / LogDuration ──────────────────────────────────────────────────

func TestLogError_IncludesErrField(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	log.LogError(context.Background(), CatApp, "operation failed", errors.New("something went wrong"),
		slog.String("op", "create"))
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
	errField, ok := entry["err"].(map[string]any)
	if !ok {
		t.Fatalf("err field missing or wrong type: %v", entry["err"])
	}
	if errField["message"] != "something went wrong" {
		t.Errorf("err.message = %v", errField["message"])
	}
	// Additional parameters go into ctx
	ctx, _ := entry["ctx"].(map[string]any)
	if ctx["op"] != "create" {
		t.Errorf("ctx.op = %v, want create", ctx)
	}
}

func TestLogDuration_Success(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	err := log.LogDuration(context.Background(), CatApp, "timed op", func(_ context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Errorf("LogDuration returned err: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", entry["level"])
	}
	ms, ok := entry["duration_ms"].(float64)
	if !ok || ms < 5 {
		t.Errorf("duration_ms = %v, want ≥ 5", entry["duration_ms"])
	}
}

func TestLogDuration_Error_RecordsBoth(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	expectedErr := errors.New("boom")
	err := log.LogDuration(context.Background(), CatApp, "failing op", func(_ context.Context) error {
		time.Sleep(5 * time.Millisecond)
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("LogDuration should return inner err, got %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
	if _, has := entry["err"]; !has {
		t.Error("err field missing")
	}
	if _, has := entry["duration_ms"]; !has {
		t.Error("duration_ms field missing")
	}
}

// ─── Rotation ────────────────────────────────────────────────────────────────

func TestRotateWriter_SizeRollover(t *testing.T) {
	dir := t.TempDir()
	rw, err := newRotateWriter(dir, "test", true, 1, 0, 5)
	if err != nil {
		t.Fatalf("newRotateWriter: %v", err)
	}
	rw.writer.SetMaxSize(20)

	data := []byte(`{"test":1}`)
	for i := 0; i < 5; i++ {
		if _, err := rw.Write(data); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	_ = rw.Close()

	base := filepath.Join(dir, "test-"+today())
	if _, err := os.Stat(base + ".jsonl"); err != nil {
		t.Errorf("main file missing: %v", err)
	}
	if _, err := os.Stat(base + "-2.jsonl"); err != nil {
		t.Errorf("rolled file missing: %v", err)
	}
}

func TestRotateWriter_ByDate_FileNameFormat(t *testing.T) {
	dir := t.TempDir()
	rw, err := newRotateWriter(dir, "app", true, 50, 30, 0)
	if err != nil {
		t.Fatalf("newRotateWriter: %v", err)
	}
	if _, err := rw.Write([]byte(`{"k":1}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = rw.Close()

	expected := filepath.Join(dir, "app-"+today()+".jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected by-date file %s, err: %v", expected, err)
	}
}

func TestRotateWriter_Cleanup_OldFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an old file from 40 days ago + a new file from 1 day ago
	oldFile := filepath.Join(dir, "app-2020-01-01.jsonl")
	newFile := filepath.Join(dir, "app-"+today()+".jsonl")
	if err := os.WriteFile(oldFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("create old: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -40)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("create new: %v", err)
	}

	// Trigger cleanup on startup (maxDays=30)
	rw, err := newRotateWriter(dir, "app", true, 50, 30, 0)
	if err != nil {
		t.Fatalf("newRotateWriter: %v", err)
	}
	_ = rw.Close()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file should be deleted, stat err: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new file should remain, stat err: %v", err)
	}
}

func TestRotateWriter_Cleanup_MaxDaysZero_Skips(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "app-2020-01-01.jsonl")
	_ = os.WriteFile(oldFile, []byte("{}"), 0o644)
	oldTime := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	// maxDays=0 should skip cleanup
	rw, err := newRotateWriter(dir, "app", true, 50, 0, 0)
	if err != nil {
		t.Fatalf("newRotateWriter: %v", err)
	}
	_ = rw.Close()

	if _, err := os.Stat(oldFile); err != nil {
		t.Errorf("maxDays=0 should preserve old file, err: %v", err)
	}
}

func TestRotateWriter_ReopenAppends(t *testing.T) {
	dir := t.TempDir()
	rw, err := newRotateWriter(dir, "test", true, 50, 30, 0)
	if err != nil {
		t.Fatalf("newRotateWriter: %v", err)
	}
	_, _ = rw.Write([]byte(`{"a":1}`))
	_ = rw.Close()

	// Reopening should append instead of overwrite
	rw2, err := newRotateWriter(dir, "test", true, 50, 30, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_, _ = rw2.Write([]byte(`{"b":2}`))
	_ = rw2.Close()

	file := filepath.Join(dir, "test-"+today()+".jsonl")
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `"a":1`) || !strings.Contains(string(data), `"b":2`) {
		t.Errorf("reopen should append, got: %s", data)
	}
}

// ─── Concurrent Writes ───────────────────────────────────────────────────────

func TestLogger_ConcurrentWrites_SingleCategory(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}

	const goroutines = 20
	const perGoroutine = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				log.Info(CatApp, "concurrent", slog.Int("goroutine", id), slog.Int("seq", j))
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	_ = log.Close()

	entries := readAllEntries(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if len(entries) != goroutines*perGoroutine {
		t.Errorf("expected %d entries, got %d (lost writes or corruption)", goroutines*perGoroutine, len(entries))
	}
}

func TestLogger_ConcurrentWrites_MultipleCategories(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false), WithLevel(slog.LevelDebug))

	const n = 30
	var wg sync.WaitGroup
	cats := []Category{CatApp, CatConfig, CatHTTP, CatWS}
	for _, cat := range cats {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(c Category, idx int) {
				defer wg.Done()
				log.Info(c, "test", slog.Int("idx", idx))
			}(cat, i)
		}
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	_ = log.Close()

	for _, cat := range cats {
		file := filepath.Join(dir, "logs", "system", string(cat)+"-"+today()+".jsonl")
		entries := readAllEntries(t, file)
		if len(entries) != n {
			t.Errorf("category %s: expected %d entries, got %d", cat, n, len(entries))
		}
	}
}

func TestLogger_ConcurrentChildLogger(t *testing.T) {
	// Different child loggers sharing the underlying writer should be safe
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			child := log.WithTraceID("trace-x")
			for j := 0; j < 10; j++ {
				child.Info(CatApp, "from child", slog.Int("id", id), slog.Int("j", j))
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	_ = log.Close()

	entries := readAllEntries(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if len(entries) != 200 {
		t.Errorf("expected 200 entries, got %d", len(entries))
	}
}

// ─── Console Handler ─────────────────────────────────────────────────────────

func TestLogger_ConsoleDisabled_FileOnly(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithFile(true))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}
	log.Info(CatApp, "file only")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	if _, err := os.Stat(appFile); err != nil {
		t.Errorf("file should exist, err: %v", err)
	}
}

func TestLogger_FileDisabled_NoFileCreated(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithFile(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}
	log.Info(CatApp, "no file")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	// No file should be created
	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	if _, err := os.Stat(appFile); !os.IsNotExist(err) {
		t.Errorf("file should NOT exist when WithFile(false), err: %v", err)
	}
}

func TestLogger_BothDisabled_NoPanic(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithFile(false))
	if err != nil {
		t.Fatalf("System(): %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	log.Info(CatApp, "nowhere")
	_ = log.Close()
}

// ─── slog.With / slog.WithGroup passthrough ──────────────────────────────────

func TestLogger_SlogWith_PropagatesAttrs(t *testing.T) {
	// Attributes injected via Child should be propagated to WithAttrs → FileHandler
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	c := log.Child(slog.String("actor_id", "worker-7"))
	c.Info(CatApp, "with attrs")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	if entry["actor_id"] != "worker-7" {
		t.Errorf("actor_id = %v, want worker-7 (WithAttrs broken)", entry["actor_id"])
	}
}

func TestLogger_SlogWith_CategoryInjected(t *testing.T) {
	// Category injected in Child should also be correctly routed
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))

	c := log.Child(slog.String("category", string(CatConfig)))
	// Explicitly passing CatApp, the category in the record should override
	c.Info(CatApp, "category routing")
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	// Explicit parameter CatApp should win (record attrs take precedence)
	appFile := filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl")
	if _, err := os.Stat(appFile); err != nil {
		t.Errorf("app file missing: %v", err)
	}
}

func TestLogger_SlogWithGroup_NoPanic(t *testing.T) {
	// Calling WithGroup directly on inner should not cause the file handler to crash
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))
	defer log.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WithGroup panicked: %v", r)
		}
	}()

	grouped := log.inner.WithGroup("mygroup")
	grouped.Info("grouped message", slog.String("key", "val"))
	time.Sleep(20 * time.Millisecond)
}

// ─── Timestamps & Format ─────────────────────────────────────────────────────

func TestLogger_TimestampISO8601(t *testing.T) {
	dir := t.TempDir()
	log, _ := System(dir, WithConsole(false))
	before := time.Now().UTC()
	log.Info(CatApp, "ts test")
	after := time.Now().UTC()
	time.Sleep(20 * time.Millisecond)
	_ = log.Close()

	entry := readFirstEntry(t, filepath.Join(dir, "logs", "system", "app-"+today()+".jsonl"))
	tsStr, _ := entry["ts"].(string)
	ts, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		t.Fatalf("parse ts %q: %v", tsStr, err)
	}
	if ts.Before(before.Add(-time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("ts %v out of window [%v, %v]", ts, before, after)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func checkJSONLFile(t *testing.T, path string, minLines int) {
	t.Helper()
	entries := readAllEntries(t, path)
	if len(entries) < minLines {
		t.Errorf("file %s: got %d entries, want ≥ %d", path, len(entries), minLines)
	}
}

func readAllEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s: %v", path, err)
	}
	defer f.Close()

	var entries []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("invalid JSONL: %v\nline: %s", err, line)
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return entries
}

func readFirstEntry(t *testing.T, path string) map[string]any {
	t.Helper()
	entries := readAllEntries(t, path)
	if len(entries) == 0 {
		t.Fatalf("no entries in %s", path)
	}
	return entries[0]
}

// ─── Stage A6: *Context methods ──────────────────────────────────────────────

func TestLogger_WithTraceID_ReturnsSameCtxIfEmpty(t *testing.T) {
	ctx := context.Background()
	got := WithTraceID(ctx, "")
	if got != ctx {
		t.Error("WithTraceID with empty id should return same ctx")
	}
}

func TestLogger_WithActorID_ReturnsSameCtxIfEmpty(t *testing.T) {
	ctx := context.Background()
	got := WithActorID(ctx, "")
	if got != ctx {
		t.Error("WithActorID with empty id should return same ctx")
	}
}

func TestLogger_TraceIDFromContext_NilCtx(t *testing.T) {
	if id := TraceIDFromContext(nil); id != "" {
		t.Errorf("TraceIDFromContext(nil) = %q, want empty", id)
	}
}

func TestLogger_InfoContext_InjectsTraceID(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	ctx := WithTraceID(context.Background(), "trace-abc123")
	log.InfoContext(ctx, CatApp, "hello")

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entry := readFirstEntry(t, path)

	if entry["trace_id"] != "trace-abc123" {
		t.Errorf("trace_id = %v, want trace-abc123", entry["trace_id"])
	}
	// Should not appear when actor_id is absent
	if _, has := entry["actor_id"]; has {
		t.Error("actor_id should not appear when not set in ctx")
	}
}

func TestLogger_InfoContext_InjectsActorID(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	ctx := WithActorID(context.Background(), "actor-xyz")
	log.InfoContext(ctx, CatApp, "hello")

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entry := readFirstEntry(t, path)

	if entry["actor_id"] != "actor-xyz" {
		t.Errorf("actor_id = %v, want actor-xyz", entry["actor_id"])
	}
}

func TestLogger_InfoContext_BothTraceAndActor(t *testing.T) {
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	ctx := WithTraceID(context.Background(), "t-1")
	ctx = WithActorID(ctx, "a-1")
	log.InfoContext(ctx, CatApp, "hello")

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entry := readFirstEntry(t, path)

	if entry["trace_id"] != "t-1" {
		t.Errorf("trace_id = %v", entry["trace_id"])
	}
	if entry["actor_id"] != "a-1" {
		t.Errorf("actor_id = %v", entry["actor_id"])
	}
}

func TestLogger_InfoContext_UserAttrOverridesCtx(t *testing.T) {
	// slog semantics: latter overrides former
	// trace_id injected by ctx comes first, user-explicit trace_id comes second, should override
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	ctx := WithTraceID(context.Background(), "from-ctx")
	log.InfoContext(ctx, CatApp, "hello", slog.String("trace_id", "from-user"))

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entry := readFirstEntry(t, path)

	// Note: The top-level trace_id is the first written value (from-ctx)
	// Because in FileHandler.applyAttr, entry[key] = val will overwrite if the key is the same
	// Actual behavior: latter overrides former (from-user overrides from-ctx)
	if entry["trace_id"] != "from-user" {
		t.Errorf("user-provided trace_id should win, got %v", entry["trace_id"])
	}
}

func TestLogger_InfoContext_NoInjection_WhenCtxBackground(t *testing.T) {
	// ctx.Background() has no trace_id/actor_id, these fields should not appear in the logs
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	log.InfoContext(context.Background(), CatApp, "hello")

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entry := readFirstEntry(t, path)

	if _, has := entry["trace_id"]; has {
		t.Error("trace_id should not appear when ctx has no trace id")
	}
	if _, has := entry["actor_id"]; has {
		t.Error("actor_id should not appear when ctx has no actor id")
	}
}

func TestLogger_AllLevelsContext(t *testing.T) {
	// Debug / Info / Warn / Error's *Context all work
	dir := t.TempDir()
	log, err := System(dir, WithConsole(false), WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	defer log.Close()

	ctx := WithTraceID(context.Background(), "t-lvl")
	log.DebugContext(ctx, CatApp, "d")
	log.InfoContext(ctx, CatApp, "i")
	log.WarnContext(ctx, CatApp, "w")
	log.ErrorContext(ctx, CatApp, "e")

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	entries := readAllEntries(t, path)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, want := range levels {
		if entries[i]["level"] != want {
			t.Errorf("entry[%d].level = %v, want %v", i, entries[i]["level"], want)
		}
		if entries[i]["trace_id"] != "t-lvl" {
			t.Errorf("entry[%d].trace_id = %v", i, entries[i]["trace_id"])
		}
	}
}