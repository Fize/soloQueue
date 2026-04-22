package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Build ─────────────────────────────────────────────────────────────

func TestBuild_WithoutTavily(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	list := Build(cfg)
	if len(list) != 9 {
		t.Errorf("Build without TavilyAPIKey returned %d tools, want 9", len(list))
	}
	for _, tool := range list {
		if tool.Name() == "web_search" {
			t.Errorf("web_search should be omitted when TavilyAPIKey empty")
		}
	}
}

func TestBuild_WithTavily(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	list := Build(cfg)
	if len(list) != 10 {
		t.Errorf("Build with TavilyAPIKey returned %d tools, want 10", len(list))
	}
	hasWebSearch := false
	for _, tool := range list {
		if tool.Name() == "web_search" {
			hasWebSearch = true
		}
	}
	if !hasWebSearch {
		t.Errorf("web_search should be included when TavilyAPIKey set")
	}
}

func TestBuild_ReturnsUniqueToolNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	seen := map[string]bool{}
	for _, tool := range Build(cfg) {
		if seen[tool.Name()] {
			t.Errorf("duplicate tool name %q", tool.Name())
		}
		seen[tool.Name()] = true
	}
}

// TestBuild_AllToolsHaveNonEmptyDescription sanity-checks that every built tool
// carries a description string (LLM reads this to pick the right tool).
func TestBuild_AllToolsHaveNonEmptyDescription(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	for _, tool := range Build(cfg) {
		if tool.Description() == "" {
			t.Errorf("tool %q has empty Description", tool.Name())
		}
	}
}

// ─── resolveSandbox ────────────────────────────────────────────────────

func TestResolveSandbox_Happy(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "a.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, err := resolveSandbox([]string{dir}, target)
	if err != nil {
		t.Fatalf("resolveSandbox: %v", err)
	}
	if abs != target {
		t.Errorf("abs = %q, want %q", abs, target)
	}
}

func TestResolveSandbox_Equal(t *testing.T) {
	dir := t.TempDir()
	// requesting the root itself is OK
	abs, err := resolveSandbox([]string{dir}, dir)
	if err != nil {
		t.Fatalf("resolveSandbox(root): %v", err)
	}
	if abs != dir {
		t.Errorf("abs = %q, want %q", abs, dir)
	}
}

func TestResolveSandbox_OutsideRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveSandbox([]string{dir}, "/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestResolveSandbox_PrefixMismatch(t *testing.T) {
	// /tmp/foo should NOT match when allowed root is /tmp/foobar
	parent := t.TempDir()
	foobar := filepath.Join(parent, "foobar")
	foo := filepath.Join(parent, "foo")
	for _, d := range []string{foobar, foo} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(foo, "a.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveSandbox([]string{foobar}, target)
	if err == nil {
		t.Error("foo/a.txt should NOT match sandbox /tmp/foobar (prefix confusion)")
	}
}

func TestResolveSandbox_TraversalAttempt(t *testing.T) {
	dir := t.TempDir()
	// ../../etc/passwd from inside dir — filepath.Abs+Clean kills the ..
	traversal := filepath.Join(dir, "..", "..", "etc", "passwd")
	_, err := resolveSandbox([]string{dir}, traversal)
	if err == nil {
		t.Error("traversal should be rejected")
	}
}

func TestResolveSandbox_EmptyPath(t *testing.T) {
	_, err := resolveSandbox([]string{t.TempDir()}, "")
	if err == nil {
		t.Error("empty path should error")
	}
}

func TestResolveSandbox_EmptyAllowList(t *testing.T) {
	_, err := resolveSandbox(nil, "/tmp/anything")
	if err == nil {
		t.Error("empty AllowedDirs should reject all paths")
	}
}

// ─── atomicWrite ───────────────────────────────────────────────────────

func TestAtomicWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	created, err := atomicWrite(path, []byte("hello"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	if !created {
		t.Errorf("created = false, want true for new file")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := atomicWrite(path, []byte("new"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	if created {
		t.Errorf("created = true, want false for overwrite")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWrite_OverwriteFalseRejectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("old"), 0o644)
	_, err := atomicWrite(path, []byte("new"), false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want file already exists", err)
	}
	// content unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("content changed: %q", got)
	}
}

func TestAtomicWrite_ParentMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexist", "a.txt")
	_, err := atomicWrite(path, []byte("x"), true)
	if err == nil || !strings.Contains(err.Error(), "parent directory missing") {
		t.Errorf("err = %v, want parent directory missing", err)
	}
}

func TestAtomicWrite_NoTmpResidueOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_, err := atomicWrite(path, []byte("x"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".soloqueue-tmp-") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

// ─── looksBinary ───────────────────────────────────────────────────────

func TestLooksBinary(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", []byte(""), false},
		{"utf8", []byte("hello world 中文"), false},
		{"nul", []byte("before\x00after"), true},
		{"nul_late_within_512", append(make([]byte, 500), 0), true},
		{"nul_after_512", append(bytes1k(), 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksBinary(c.in); got != c.want {
				t.Errorf("looksBinary = %v, want %v", got, c.want)
			}
		})
	}
}

// bytes1k returns 1024 bytes of 'a' for testing
func bytes1k() []byte {
	b := make([]byte, 1024)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// ─── readFileCapped ────────────────────────────────────────────────────

func TestReadFileCapped_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	data, err := readFileCapped(path, 100)
	if err != nil {
		t.Fatalf("readFileCapped: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
}

func TestReadFileCapped_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, bytes1k(), 0o644)
	_, err := readFileCapped(path, 100)
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Errorf("err = %v, want file too large", err)
	}
}

func TestReadFileCapped_NoLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, bytes1k(), 0o644)
	data, err := readFileCapped(path, 0)
	if err != nil {
		t.Fatalf("readFileCapped no-limit: %v", err)
	}
	if len(data) != 1024 {
		t.Errorf("len = %d, want 1024", len(data))
	}
}

func TestReadFileCapped_Missing(t *testing.T) {
	_, err := readFileCapped("/nonexistent/path/xyz.txt", 100)
	if err == nil {
		t.Error("missing file should error")
	}
}

// ─── ctxErrOrNil ───────────────────────────────────────────────────────

func TestCtxErrOrNil(t *testing.T) {
	if err := ctxErrOrNil(nil); err != nil {
		t.Errorf("ctxErrOrNil(nil) = %v, want nil", err)
	}
}

// ─── DefaultConfig ─────────────────────────────────────────────────────

func TestDefaultConfig_SaneValues(t *testing.T) {
	c := DefaultConfig()
	if c.MaxFileSize <= 0 || c.MaxWriteSize <= 0 {
		t.Errorf("MaxFileSize=%d MaxWriteSize=%d should be positive", c.MaxFileSize, c.MaxWriteSize)
	}
	if !c.HTTPBlockPrivate {
		t.Error("HTTPBlockPrivate default should be true")
	}
	if c.TavilyEndpoint == "" {
		t.Error("TavilyEndpoint should have default")
	}
}
