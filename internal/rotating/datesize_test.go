package rotating

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDateSizeWriter_WritesTodayFile(t *testing.T) {
	dir := t.TempDir()
	now := fixedTime("2026-05-08")
	w, err := OpenDateSize(dir, "tool", 0, 15, WithDateSizeNow(now))
	if err != nil {
		t.Fatalf("OpenDateSize: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("line")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	assertFileContent(t, filepath.Join(dir, "tool-2026-05-08.jsonl"), "line\n")
}

func TestDateSizeWriter_RotatesBySize(t *testing.T) {
	dir := t.TempDir()
	now := fixedTime("2026-05-08")
	w, err := OpenDateSize(dir, "tool", 10, 15, WithDateSizeNow(now))
	if err != nil {
		t.Fatalf("OpenDateSize: %v", err)
	}
	defer w.Close()

	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte("1234")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	assertFileContent(t, filepath.Join(dir, "tool-2026-05-08.jsonl"), "1234\n1234\n")
	assertFileContent(t, filepath.Join(dir, "tool-2026-05-08-2.jsonl"), "1234\n")
}

func TestDateSizeWriter_RotatesByDate(t *testing.T) {
	dir := t.TempDir()
	cur := mustParseDate("2026-05-08")
	w, err := OpenDateSize(dir, "tool", 0, 15, WithDateSizeNow(func() time.Time { return cur }))
	if err != nil {
		t.Fatalf("OpenDateSize: %v", err)
	}
	defer w.Close()

	_, _ = w.Write([]byte("first"))
	cur = mustParseDate("2026-05-09")
	_, _ = w.Write([]byte("second"))

	assertFileContent(t, filepath.Join(dir, "tool-2026-05-08.jsonl"), "first\n")
	assertFileContent(t, filepath.Join(dir, "tool-2026-05-09.jsonl"), "second\n")
}

func TestDateSizeWriter_ReopenStartsNextSeqWhenLatestFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-2026-05-08.jsonl")
	if err := os.WriteFile(path, []byte("1234567890"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := OpenDateSize(dir, "tool", 10, 15, WithDateSizeNow(fixedTime("2026-05-08")))
	if err != nil {
		t.Fatalf("OpenDateSize: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	assertFileContent(t, filepath.Join(dir, "tool-2026-05-08-2.jsonl"), "new\n")
}

func TestDateSizeWriter_CleansOldFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "tool-2026-04-20.jsonl")
	keepPath := filepath.Join(dir, "tool-2026-04-24.jsonl")
	otherPath := filepath.Join(dir, "other-2026-04-20.jsonl")
	for _, p := range []string{oldPath, keepPath, otherPath} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	w, err := OpenDateSize(dir, "tool", 0, 15, WithDateSizeNow(fixedTime("2026-05-08")))
	if err != nil {
		t.Fatalf("OpenDateSize: %v", err)
	}
	defer w.Close()

	assertMissing(t, oldPath)
	assertExists(t, keepPath)
	assertExists(t, otherPath)
}

func TestListDateSizeFiles_Order(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"tool-2026-05-09.jsonl",
		"tool-2026-05-08-2.jsonl",
		"tool-2026-05-08.jsonl",
		"other-2026-05-08.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	files, err := ListDateSizeFiles(dir, "tool")
	if err != nil {
		t.Fatalf("ListDateSizeFiles: %v", err)
	}
	expected := []string{
		filepath.Join(dir, "tool-2026-05-08.jsonl"),
		filepath.Join(dir, "tool-2026-05-08-2.jsonl"),
		filepath.Join(dir, "tool-2026-05-09.jsonl"),
	}
	if len(files) != len(expected) {
		t.Fatalf("got %d files, want %d: %v", len(files), len(expected), files)
	}
	for i := range expected {
		if files[i] != expected[i] {
			t.Errorf("files[%d] = %q, want %q", i, files[i], expected[i])
		}
	}
}

func fixedTime(date string) func() time.Time {
	t := mustParseDate(date)
	return func() time.Time { return t }
}

func mustParseDate(date string) time.Time {
	t, err := time.ParseInLocation(dateLayout, date, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, err=%v", path, err)
	}
}
