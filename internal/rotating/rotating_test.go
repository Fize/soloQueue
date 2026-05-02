package rotating

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ─── Open & Close ────────────────────────────────────────────────────────────

func TestOpen_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "deep")
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestOpen_CreatesActiveFile(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	expected := filepath.Join(dir, "timeline.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("active file not created: %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// os.File.Close() on already-closed file returns error; that's expected
	if err := w.Close(); err == nil {
		t.Log("second Close succeeded (implementation detail)")
	}
}

// ─── Write ───────────────────────────────────────────────────────────────────

func TestWrite_AppendsNewline(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	data := []byte(`{"hello":"world"}`)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	expected := `{"hello":"world"}` + "\n"
	if string(content) != expected {
		t.Errorf("got %q, want %q", string(content), expected)
	}
}

func TestWrite_NoDoubleNewline(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	data := []byte(`{"hello":"world"}` + "\n")
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	expected := `{"hello":"world"}` + "\n"
	if string(content) != expected {
		t.Errorf("got %q, want %q", string(content), expected)
	}
}

func TestWrite_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("line")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	content, err := os.ReadFile(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// 5 lines, each "line\n"
	expected := "line\nline\nline\nline\nline\n"
	if string(content) != expected {
		t.Errorf("got %q, want %q", string(content), expected)
	}
}

// ─── Size-based Rotation ─────────────────────────────────────────────────────

func TestRotation_SingleRollover(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 20, 0) // 20 bytes max
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// Write enough data to trigger rotation
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("line")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// Active file should exist
	if _, err := os.Stat(filepath.Join(dir, "test.jsonl")); err != nil {
		t.Errorf("active file missing: %v", err)
	}
	// .1 rotated file should exist
	if _, err := os.Stat(filepath.Join(dir, "test.jsonl.1")); err != nil {
		t.Errorf("rolled .1 file missing: %v", err)
	}
}

func TestRotation_MultipleRollover(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 15, 0) // 15 bytes max
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// 写入大量数据触发多次轮转
	for i := 0; i < 20; i++ {
		if _, err := w.Write([]byte("data")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// 应有 .1, .2 等轮转文件
	found := 0
	for n := 1; n <= 10; n++ {
		p := filepath.Join(dir, fmt.Sprintf("test.jsonl.%d", n))
		if _, err := os.Stat(p); err == nil {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected ≥ 2 rotated files, found %d", found)
	}
}

// ─── trimFiles ───────────────────────────────────────────────────────────────

func TestTrimFiles_DeletesOldFiles(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 15, 2) // maxFiles=2
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// 写入大量数据触发多次轮转
	for i := 0; i < 30; i++ {
		if _, err := w.Write([]byte("dat")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// 超过 maxFiles 的文件应被删除
	// 允许 .1 和 .2 存在，.3 及以上应被删除
	for n := 3; n <= 10; n++ {
		p := filepath.Join(dir, fmt.Sprintf("test.jsonl.%d", n))
		if _, err := os.Stat(p); err == nil {
			t.Errorf("file %s should have been trimmed", p)
		}
	}
}

func TestTrimFiles_MaxFilesZero_NoTrim(t *testing.T) {
	dir := t.TempDir()
	// 先手动创建一些编号文件
	for n := 1; n <= 5; n++ {
		p := filepath.Join(dir, fmt.Sprintf("test.jsonl.%d", n))
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	w, err := Open(dir, "test", 0, 0) // maxFiles=0 = no limit
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// 手动写一条触发 trimFiles 路径
	w.SetMaxSize(1)
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 所有 5 个编号文件应保留
	for n := 1; n <= 5; n++ {
		p := filepath.Join(dir, fmt.Sprintf("test.jsonl.%d", n))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file %s should exist (maxFiles=0 = no trim)", p)
		}
	}
}

// ─── SetMaxSize ──────────────────────────────────────────────────────────────

func TestSetMaxSize_TriggersRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0) // no limit initially
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	// 写入一些数据
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("line")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// 设置极小的 maxSize，下次写入应触发轮转
	w.SetMaxSize(5)

	if _, err := w.Write([]byte("trigger")); err != nil {
		t.Fatalf("Write after SetMaxSize: %v", err)
	}

	// 应有轮转文件
	if _, err := os.Stat(filepath.Join(dir, "test.jsonl.1")); err != nil {
		t.Errorf("rolled .1 file missing after SetMaxSize: %v", err)
	}
}

// ─── Reopen Appends ──────────────────────────────────────────────────────────

func TestReopen_AppendsNotOverwrites(t *testing.T) {
	dir := t.TempDir()

	w1, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	w1.Write([]byte("first"))
	w1.Close()

	w2, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	w2.Write([]byte("second"))
	w2.Close()

	content, err := os.ReadFile(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)
	if s != "first\nsecond\n" {
		t.Errorf("got %q, want %q", s, "first\nsecond\n")
	}
}

// ─── ListFiles ───────────────────────────────────────────────────────────────

func TestListFiles_Order(t *testing.T) {
	dir := t.TempDir()
	// 创建轮转文件 + 活跃文件（实际命名: baseName.jsonl.N）
	os.WriteFile(filepath.Join(dir, "tl.jsonl.3"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "tl.jsonl.1"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "tl.jsonl.2"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "tl.jsonl"), []byte{}, 0o644)
	// 不相关文件
	os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte{}, 0o644)

	files, err := ListFiles(dir, "tl")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	// 顺序：编号大的（老）→ 编号小的（新）→ 活跃文件（最新）
	expected := []string{
		filepath.Join(dir, "tl.jsonl.3"),
		filepath.Join(dir, "tl.jsonl.2"),
		filepath.Join(dir, "tl.jsonl.1"),
		filepath.Join(dir, "tl.jsonl"),
	}
	if len(files) != len(expected) {
		t.Fatalf("got %d files, want %d: %v", len(files), len(expected), files)
	}
	for i, want := range expected {
		if files[i] != want {
			t.Errorf("files[%d] = %q, want %q", i, files[i], want)
		}
	}
}

func TestListFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	files, err := ListFiles(dir, "tl")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestListFiles_NonexistentDir(t *testing.T) {
	files, err := ListFiles("/nonexistent/path/xyz", "tl")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for nonexistent dir, got %d", len(files))
	}
}

func TestListFiles_OnlyMatchesBaseName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "timeline.jsonl"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "timeline.jsonl.1"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "timeline-other.jsonl"), []byte{}, 0o644)
	os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte{}, 0o644)

	files, err := ListFiles(dir, "timeline")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	// 不应匹配 timeline-other.jsonl
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(files), files)
	}
}

// ─── CurrentPath ─────────────────────────────────────────────────────────────

func TestCurrentPath(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "mylog", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	expected := filepath.Join(dir, "mylog.jsonl")
	if w.CurrentPath() != expected {
		t.Errorf("CurrentPath = %q, want %q", w.CurrentPath(), expected)
	}
}

// ─── Concurrent Writes ───────────────────────────────────────────────────────

func TestWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	const goroutines = 20
	const perGoroutine = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if _, err := w.Write([]byte("data")); err != nil {
					t.Errorf("goroutine %d write %d: %v", id, j, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// 读取并统计行数
	content, err := os.ReadFile(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := 0
	for _, b := range content {
		if b == '\n' {
			lines++
		}
	}
	if lines != goroutines*perGoroutine {
		t.Errorf("expected %d lines, got %d", goroutines*perGoroutine, lines)
	}
}

// ─── Zero maxSize = No Rotation ──────────────────────────────────────────────

func TestZeroMaxSize_NoRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "test", 0, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	for i := 0; i < 100; i++ {
		if _, err := w.Write([]byte("data")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// 不应有轮转文件
	entries, _ := os.ReadDir(dir)
	rotated := 0
	for _, e := range entries {
		if e.Name() != "test.jsonl" {
			rotated++
		}
	}
	if rotated > 0 {
		t.Errorf("expected no rotated files with maxSize=0, found %d", rotated)
	}
}
