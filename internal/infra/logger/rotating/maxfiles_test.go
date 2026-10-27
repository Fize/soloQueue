package rotating

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDateSizeMaxFilesNumericAndRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	for _, seq := range []int{1, 2, 3, 9, 10, 11} {
		if err := os.WriteFile(filepath.Join(dir, dateSizeFileName("tool", "2026-10-26", seq)), []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.jsonl"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := OpenDateSize(dir, "tool", 2, 15, WithDateSizeMaxFiles(2), WithDateSizeNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	files, _ := ListDateSizeFiles(dir, "tool")
	if len(files) != 3 {
		t.Fatalf("files %v", files)
	}
	if filepath.Base(files[0]) != "tool-2026-10-26-10.jsonl" {
		t.Fatalf("numeric order wrong %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.jsonl")); err != nil {
		t.Fatal("unrelated file removed")
	}
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	files, _ = ListDateSizeFiles(dir, "tool")
	if len(files) != 3 {
		t.Fatalf("write count %d", len(files))
	}
	if _, err := os.Stat(w.CurrentPath()); err != nil {
		t.Fatal("active file removed")
	}
}
func TestDateSizeUnlimitedAndInclusiveDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.Local)
	for _, date := range []string{"2026-10-10", "2026-10-11", "2026-10-12"} {
		_ = os.WriteFile(filepath.Join(dir, dateSizeFileName("tool", date, 1)), []byte("x"), 0600)
	}
	w, err := OpenDateSize(dir, "tool", 0, 15, WithDateSizeNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	files, _ := ListDateSizeFiles(dir, "tool")
	if len(files) != 2 {
		t.Fatalf("inclusive15 days expected12..26 got %v", files)
	}
}
func TestDateSizePolicyAndActiveProtection(t *testing.T) {
	dir := t.TempDir()
	policy := Limits{MaxBytes: 2, MaxDays: 0, MaxFiles: 0}
	unregister := RegisterPolicy(dir, func(string, string) Limits { return policy })
	defer unregister()
	w, err := OpenDateSize(dir, "a", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 5; i++ {
		_, _ = w.Write([]byte("x"))
	}
	files, _ := ListDateSizeFiles(dir, "a")
	if len(files) != 5 {
		t.Fatalf("zero count limited %v", files)
	}
	policy.MaxFiles = 1
	_, _ = w.Write([]byte("x"))
	files, _ = ListDateSizeFiles(dir, "a")
	if len(files) != 2 {
		t.Fatalf("live policy ineffective %v", files)
	}
}

func TestActiveRemovalAndIdempotentClose(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenDateSize(dir, "shared", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenDateSize(dir, "shared", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := first.CurrentPath()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	if err := RemoveInactive(path, func() error { return os.Remove(path) }); err == nil {
		t.Fatal("removed another writer's file")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RemoveInactive(path, func() error { return os.Remove(path) }); err != nil {
		t.Fatal(err)
	}
}
func TestPolicyRegistrationRestoresPreviousResolver(t *testing.T) {
	dir := t.TempDir()
	one := RegisterPolicy(dir, func(string, string) Limits { return Limits{MaxBytes: 10} })
	defer one()
	two := RegisterPolicy(dir, func(string, string) Limits { return Limits{MaxBytes: 20} })
	if p, _ := resolvePolicy(dir, "a"); p.MaxBytes != 20 {
		t.Fatal("latest resolver not selected")
	}
	two()
	if p, _ := resolvePolicy(dir, "a"); p.MaxBytes != 10 {
		t.Fatal("older live resolver not restored")
	}
}
