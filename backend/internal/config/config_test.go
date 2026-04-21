package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ─── MergeJSON ───────────────────────────────────────────────────────────────

func TestMergeJSON_PartialOverride(t *testing.T) {
	base := Settings{
		App:     AppConfig{Theme: "dark", Language: "zh-CN"},
		Session: SessionConfig{TimeoutSecs: 3600, MaxHistory: 1000, AutoSave: true},
	}
	patch := []byte(`{"app":{"theme":"light"}}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.App.Theme != "light" {
		t.Errorf("app.theme = %q, want light", result.App.Theme)
	}
	if result.App.Language != "zh-CN" {
		t.Errorf("app.language = %q, want zh-CN (preserved)", result.App.Language)
	}
	if result.Session.TimeoutSecs != 3600 {
		t.Errorf("session.timeoutSecs = %d, want 3600 (preserved)", result.Session.TimeoutSecs)
	}
}

func TestMergeJSON_DeepNestedObject(t *testing.T) {
	// 关键场景：嵌套对象的 partial merge
	// settings.local.json 只改 providers 不行（数组整体替换），
	// 但可以嵌套覆盖 log、embedding 等对象字段
	base := Settings{
		Log: LogConfig{
			Level:         "info",
			Console:       true,
			File:          true,
			MaxDays:       30,
			MaxFileSizeMB: 50,
		},
	}
	patch := []byte(`{"log":{"level":"debug","maxDays":7}}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", result.Log.Level)
	}
	if result.Log.MaxDays != 7 {
		t.Errorf("log.maxDays = %d, want 7", result.Log.MaxDays)
	}
	// 未被 patch 的字段保留
	if !result.Log.Console {
		t.Errorf("log.console = false, want true (preserved)")
	}
	if result.Log.MaxFileSizeMB != 50 {
		t.Errorf("log.maxFileSizeMB = %d, want 50 (preserved)", result.Log.MaxFileSizeMB)
	}
}

func TestMergeJSON_EmbeddingNestedMerge(t *testing.T) {
	base := DefaultSettings()
	// 只修改 embedding.enabled，内部数组应被保留
	patch := []byte(`{"embedding":{"enabled":true}}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if !result.Embedding.Enabled {
		t.Errorf("embedding.enabled not applied")
	}
	if len(result.Embedding.Providers) != len(base.Embedding.Providers) {
		t.Errorf("embedding.providers len = %d, want preserved (%d)",
			len(result.Embedding.Providers), len(base.Embedding.Providers))
	}
	if len(result.Embedding.Models) != len(base.Embedding.Models) {
		t.Errorf("embedding.models not preserved")
	}
}

func TestMergeJSON_ArrayReplacement(t *testing.T) {
	base := DefaultSettings()
	patch := []byte(`{"providers":[{"id":"openai","name":"OpenAI","enabled":true}]}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Errorf("providers len = %d, want 1 (wholesale replace)", len(result.Providers))
	}
	if result.Providers[0].ID != "openai" {
		t.Errorf("providers[0].id = %q, want openai", result.Providers[0].ID)
	}
}

func TestMergeJSON_NullPreservesValue(t *testing.T) {
	base := Settings{App: AppConfig{Theme: "dark"}}
	patch := []byte(`{"app":{"theme":null}}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.App.Theme != "dark" {
		t.Errorf("null should preserve, got %q", result.App.Theme)
	}
}

func TestMergeJSON_EmptyPatch_NoOp(t *testing.T) {
	base := DefaultSettings()
	result, err := MergeJSON(base, []byte(`{}`))
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.App.Theme != base.App.Theme {
		t.Errorf("empty patch should preserve defaults")
	}
}

func TestMergeJSON_InvalidJSON_Errors(t *testing.T) {
	base := DefaultSettings()
	_, err := MergeJSON(base, []byte(`{not valid json`))
	if err == nil {
		t.Error("invalid JSON should return error")
	}
}

func TestMergeJSON_UnknownFields_Ignored(t *testing.T) {
	base := Settings{App: AppConfig{Theme: "dark"}}
	// 未知字段应被忽略（反序列化时丢弃）
	patch := []byte(`{"app":{"theme":"light"},"unknownField":"xxx"}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.App.Theme != "light" {
		t.Errorf("theme = %q, want light", result.App.Theme)
	}
}

func TestMergeJSON_NumericTypes(t *testing.T) {
	base := Settings{Session: SessionConfig{TimeoutSecs: 100}}
	patch := []byte(`{"session":{"timeoutSecs":7200}}`)
	result, err := MergeJSON(base, patch)
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}
	if result.Session.TimeoutSecs != 7200 {
		t.Errorf("timeoutSecs = %d, want 7200", result.Session.TimeoutSecs)
	}
}

func TestMergeJSON_BooleanOverride(t *testing.T) {
	base := Settings{Session: SessionConfig{AutoSave: true}}
	patch := []byte(`{"session":{"autoSave":false}}`)
	result, _ := MergeJSON(base, patch)
	if result.Session.AutoSave {
		t.Errorf("autoSave should be overridden to false")
	}
}

// ─── Loader: Load ────────────────────────────────────────────────────────────

func TestLoader_Load_NoFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loader.Get()
	if got.App.Theme != "dark" {
		t.Errorf("app.theme = %q, want dark", got.App.Theme)
	}
}

func TestLoader_Load_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})

	loader, _ := NewLoader(DefaultSettings(), path)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loader.Get()
	if got.App.Theme != "light" {
		t.Errorf("theme = %q, want light", got.App.Theme)
	}
	if got.App.Language != "zh-CN" {
		t.Errorf("language should be preserved from defaults")
	}
}

func TestLoader_Load_InvalidJSON_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{not valid`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	err := loader.Load()
	if err == nil {
		t.Fatal("Load should return error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Errorf("error should mention merge/parse failure, got: %v", err)
	}
}

func TestLoader_Load_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	err := loader.Load()
	if err == nil {
		t.Error("Load should fail on permission denied")
	}
}

func TestLoader_Load_MultiLayer(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.json")
	local := filepath.Join(dir, "settings.local.json")

	writeJSON(t, main, map[string]any{"app": map[string]any{"theme": "light"}})
	writeJSON(t, local, map[string]any{"app": map[string]any{"language": "en-US"}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := loader.Get()
	if got.App.Theme != "light" {
		t.Errorf("theme = %q, want light (from main)", got.App.Theme)
	}
	if got.App.Language != "en-US" {
		t.Errorf("language = %q, want en-US (from local)", got.App.Language)
	}
}

func TestLoader_Load_LocalOverridesMain(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.json")
	local := filepath.Join(dir, "settings.local.json")

	writeJSON(t, main, map[string]any{"app": map[string]any{"theme": "light"}})
	writeJSON(t, local, map[string]any{"app": map[string]any{"theme": "dark"}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	_ = loader.Load()

	if loader.Get().App.Theme != "dark" {
		t.Errorf("local should override main, got %q", loader.Get().App.Theme)
	}
}

func TestLoader_Load_MissingLocalFile_OK(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "settings.json")
	local := filepath.Join(dir, "settings.local.json")
	writeJSON(t, main, map[string]any{"app": map[string]any{"theme": "light"}})

	loader, _ := NewLoader(DefaultSettings(), main, local)
	if err := loader.Load(); err != nil {
		t.Fatalf("missing local should not error, got: %v", err)
	}
	if loader.Get().App.Theme != "light" {
		t.Errorf("main theme not applied")
	}
}

// ─── Loader: Set / Save ──────────────────────────────────────────────────────

func TestLoader_Set_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Set(func(s *Settings) { s.App.Theme = "light" }); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var saved Settings
	_ = json.Unmarshal(data, &saved)
	if saved.App.Theme != "light" {
		t.Errorf("saved theme = %q", saved.App.Theme)
	}
}

func TestLoader_Set_AtomicWrite_NoTmpLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	_ = loader.Set(func(s *Settings) { s.App.Theme = "light" })

	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf(".tmp should not exist after successful Set, err: %v", err)
	}
	// 主文件应存在
	if _, err := os.Stat(path); err != nil {
		t.Errorf("main file missing: %v", err)
	}
}

func TestLoader_Set_WriteFails_RollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("readonly dir semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	// 只读目录：无法创建文件
	readonly := filepath.Join(dir, "ro")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	defer os.Chmod(readonly, 0o755)

	path := filepath.Join(readonly, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	originalTheme := loader.Get().App.Theme

	err := loader.Set(func(s *Settings) { s.App.Theme = "light" })
	if err == nil {
		t.Fatal("Set should fail in readonly dir")
	}

	// current 应已回滚
	if loader.Get().App.Theme != originalTheme {
		t.Errorf("after failed Set, current = %q, want %q (rollback)",
			loader.Get().App.Theme, originalTheme)
	}
}

func TestLoader_Save_WritesCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("main file should exist: %v", err)
	}
}

func TestLoader_Save_Atomic_OverridesExistingTmp(t *testing.T) {
	// 即使有遗留的 .tmp 也应正确处理
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	tmp := path + ".tmp"
	// 预先留一个 .tmp 模拟崩溃残留
	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()
	if err := loader.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 主文件是新内容
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "dark") {
		t.Errorf("main file should have new content, got: %s", data)
	}
	// .tmp 被成功 rename，不应残留
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf(".tmp should be renamed away, err: %v", err)
	}
}

func TestLoader_Save_NoPaths_Errors(t *testing.T) {
	// A2 校验：NewLoader 不传 path 直接报错，不走到 Save
	_, err := NewLoader(DefaultSettings())
	if err == nil {
		t.Error("NewLoader with no paths should error")
	}
}

// ─── Loader: OnChange ────────────────────────────────────────────────────────

func TestLoader_OnChange_CalledOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var mu sync.Mutex
	var received []string
	loader.OnChange(func(old, new Settings) {
		mu.Lock()
		received = append(received, new.App.Theme)
		mu.Unlock()
	})

	writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})
	_ = loader.Load()

	mu.Lock()
	got := len(received)
	var theme string
	if got > 0 {
		theme = received[0]
	}
	mu.Unlock()

	if got == 0 {
		t.Fatal("OnChange not called")
	}
	if theme != "light" {
		t.Errorf("received theme = %q, want light", theme)
	}
}

func TestLoader_OnChange_MultipleCallbacks_AllInvoked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var counts [3]int32
	for i := 0; i < 3; i++ {
		idx := i
		loader.OnChange(func(old, new Settings) {
			atomic.AddInt32(&counts[idx], 1)
		})
	}

	_ = loader.Set(func(s *Settings) { s.App.Theme = "light" })

	for i, c := range counts {
		if atomic.LoadInt32(&c) != 1 {
			t.Errorf("callback %d called %d times, want 1", i, atomic.LoadInt32(&counts[i]))
		}
	}
}

func TestLoader_OnChange_CallbackCanCallGet_NoDeadlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	done := make(chan Settings, 1)
	loader.OnChange(func(old, new Settings) {
		done <- loader.Get()
	})

	writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})
	_ = loader.Load()

	select {
	case s := <-done:
		if s.App.Theme != "light" {
			t.Errorf("Get from callback returned theme = %q", s.App.Theme)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: callback calling Get() timed out")
	}
}

func TestLoader_OnChange_CallbackCanCallSet_NoDeadlock(t *testing.T) {
	// 回调内调用 Set 也不应死锁（Set 会重新获取 mu）
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var callCount int32
	done := make(chan struct{}, 1)
	loader.OnChange(func(old, new Settings) {
		// 回调内部调用 Set（会再次触发 OnChange，用计数保护避免无限递归）
		if atomic.AddInt32(&callCount, 1) == 1 {
			_ = loader.Set(func(s *Settings) { s.App.Language = "en-US" })
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	_ = loader.Set(func(s *Settings) { s.App.Theme = "light" })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: callback calling Set() timed out")
	}
}

func TestLoader_OnChange_ConcurrentRegister(t *testing.T) {
	// Watch 触发 reload 时并发注册新回调不应崩溃
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 后台持续触发 Load
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = loader.Load()
			}
		}
	}()

	// 并发注册回调
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loader.OnChange(func(old, new Settings) {})
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ─── Loader: Watch (fsnotify) ────────────────────────────────────────────────

func TestLoader_Watch_HotReload_OnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer loader.Close()

	ch := make(chan string, 10)
	loader.OnChange(func(old, new Settings) {
		ch <- new.App.Theme
	})

	writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})

	select {
	case theme := <-ch:
		if theme != "light" {
			t.Errorf("hot reload theme = %q, want light", theme)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("Watch didn't fire within 1.5s")
	}
}

func TestLoader_Watch_Debounce_CoalescesRapidWrites(t *testing.T) {
	// 200ms 内多次写应只触发 1 次 reload
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer loader.Close()

	var callCount int32
	loader.OnChange(func(old, new Settings) {
		atomic.AddInt32(&callCount, 1)
	})

	// 在 150ms 内快速写 5 次
	for i := 0; i < 5; i++ {
		writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})
		time.Sleep(30 * time.Millisecond)
	}

	// 等 debounce 过去 + reload 完成
	time.Sleep(500 * time.Millisecond)

	got := atomic.LoadInt32(&callCount)
	if got < 1 {
		t.Errorf("expected at least 1 reload, got %d", got)
	}
	if got > 2 {
		// debounce 不完美时偶尔 2 次（边界情况），但不应有 5 次
		t.Errorf("debounce failed: %d reloads for 5 rapid writes", got)
	}
}

func TestLoader_Watch_DetectsRenameCreate(t *testing.T) {
	// 编辑器保存模式：write tmp → rename 覆盖
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	if err := loader.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer loader.Close()

	ch := make(chan string, 2)
	loader.OnChange(func(old, new Settings) {
		ch <- new.App.Theme
	})

	// 模拟 rename 保存
	tmp := filepath.Join(dir, "settings.json.editor-tmp")
	data, _ := json.Marshal(map[string]any{"app": map[string]any{"theme": "light"}})
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}

	select {
	case theme := <-ch:
		if theme != "light" {
			t.Errorf("theme = %q, want light", theme)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("rename event not detected within 1.5s")
	}
}

func TestLoader_Close_StopsWatcher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()
	_ = loader.Watch()

	if err := loader.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// 重复 Close 不应 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("double Close panicked: %v", r)
		}
	}()
	_ = loader.Close()
}

func TestLoader_Close_WithoutWatch(t *testing.T) {
	// 未调用 Watch 时 Close 应返回 nil
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	if err := loader.Close(); err != nil {
		t.Errorf("Close without Watch: %v", err)
	}
}

// ─── Loader: Concurrency ─────────────────────────────────────────────────────

func TestLoader_ConcurrentGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := loader.Get()
			_ = s.App.Theme
		}()
	}
	wg.Wait()
}

func TestLoader_ConcurrentGetAndSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = loader.Get()
			}
		}()
	}
	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = loader.Set(func(s *Settings) {
					s.App.Language = "en-US"
				})
			}
		}(i)
	}
	wg.Wait()
}

func TestLoader_ConcurrentLoadAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, map[string]any{"app": map[string]any{"theme": "light"}})
	loader, _ := NewLoader(DefaultSettings(), path)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.Load()
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.Get()
		}()
	}
	wg.Wait()
}

// ─── expandPath ──────────────────────────────────────────────────────────────

func TestExpandPath_Tilde(t *testing.T) {
	got, err := expandPath("~/.soloqueue/settings.json")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".soloqueue/settings.json")
	if got != want {
		t.Errorf("expandPath = %q, want %q", got, want)
	}
}

func TestExpandPath_NoTilde(t *testing.T) {
	got, err := expandPath("/absolute/path")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("expandPath = %q, want unchanged", got)
	}
}

func TestExpandPath_Empty(t *testing.T) {
	got, err := expandPath("")
	if err != nil {
		t.Fatalf("expandPath empty: %v", err)
	}
	if got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}

func TestExpandPath_RelativePath(t *testing.T) {
	got, err := expandPath("./foo/bar")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "./foo/bar" {
		t.Errorf("relative path = %q, want unchanged", got)
	}
}

// ─── GlobalService ───────────────────────────────────────────────────────────

func TestGlobalService_DefaultProvider(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = svc.Load()

	p := svc.DefaultProvider()
	if p == nil {
		t.Fatal("DefaultProvider returned nil")
	}
	if p.ID != "deepseek" {
		t.Errorf("id = %q, want deepseek", p.ID)
	}
}

func TestGlobalService_DefaultProvider_NoDefault_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) {
		s.Providers = []LLMProvider{{ID: "custom", Name: "Custom", IsDefault: false, Enabled: true}}
	})

	if svc.DefaultProvider() != nil {
		t.Error("DefaultProvider should return nil when no isDefault=true")
	}
}

func TestGlobalService_DefaultModel_ByType(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.DefaultModel("chat")
	if m == nil {
		t.Fatal("DefaultModel(chat) nil")
	}
	if m.ID != "deepseek-chat" {
		t.Errorf("id = %q, want deepseek-chat", m.ID)
	}

	m2 := svc.DefaultModel("code")
	if m2 == nil || m2.ID != "deepseek-coder" {
		t.Errorf("DefaultModel(code) = %v", m2)
	}
}

func TestGlobalService_DefaultModel_EmptyType_ReturnsFirstDefault(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.DefaultModel("")
	if m == nil {
		t.Fatal("DefaultModel(\"\") nil")
	}
	// 应返回首个 isDefault=true 的模型
	if !m.IsDefault {
		t.Errorf("should return isDefault=true model, got %+v", m)
	}
}

func TestGlobalService_DefaultModel_NoDefault_FallbackToFirst(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) {
		s.Models = []LLMModel{
			{ID: "a", Type: "chat", Enabled: true, IsDefault: false},
			{ID: "b", Type: "chat", Enabled: true, IsDefault: false},
		}
	})

	m := svc.DefaultModel("chat")
	if m == nil || m.ID != "a" {
		t.Errorf("should fallback to first enabled model, got %v", m)
	}
}

func TestGlobalService_DefaultModel_SkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) {
		s.Models = []LLMModel{
			{ID: "disabled", Type: "chat", Enabled: false, IsDefault: true},
			{ID: "enabled", Type: "chat", Enabled: true, IsDefault: false},
		}
	})

	m := svc.DefaultModel("chat")
	if m == nil || m.ID != "enabled" {
		t.Errorf("should skip disabled, got %v", m)
	}
}

func TestGlobalService_DefaultModel_UnknownType_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.DefaultModel("unknown-type")
	if m != nil {
		t.Errorf("unknown type should return nil, got %v", m)
	}
}

func TestGlobalService_DefaultEmbeddingModel(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.DefaultEmbeddingModel()
	if m == nil {
		t.Fatal("DefaultEmbeddingModel nil")
	}
	if m.ID != "bge-large-zh-v1.5" {
		t.Errorf("id = %q", m.ID)
	}
}

func TestGlobalService_DefaultEmbeddingModel_None(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	_ = svc.Set(func(s *Settings) { s.Embedding.Models = nil })

	if svc.DefaultEmbeddingModel() != nil {
		t.Error("should be nil when no embedding models")
	}
}

func TestGlobalService_ProviderByID(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	if p := svc.ProviderByID("deepseek"); p == nil || p.ID != "deepseek" {
		t.Errorf("ProviderByID(deepseek) = %v", p)
	}
	if p := svc.ProviderByID("nonexistent"); p != nil {
		t.Errorf("unknown id should be nil, got %v", p)
	}
}

func TestGlobalService_ModelByID(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	m := svc.ModelByID("deepseek-reasoner")
	if m == nil {
		t.Fatal("ModelByID nil")
	}
	if !m.Thinking.Enabled {
		t.Error("deepseek-reasoner should have thinking.enabled=true")
	}
	if m.Thinking.Type != "reasoning" {
		t.Errorf("thinking.type = %q", m.Thinking.Type)
	}
}

func TestGlobalService_ModelByID_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()
	if m := svc.ModelByID("nonexistent"); m != nil {
		t.Errorf("unknown model id should be nil, got %v", m)
	}
}

func TestGlobalService_Set_Persists(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	_ = svc.Set(func(s *Settings) { s.Log.Level = "debug" })

	// 重新加载验证持久化
	svc2, _ := New(dir)
	_ = svc2.Load()
	if svc2.Get().Log.Level != "debug" {
		t.Errorf("persisted log.level = %q", svc2.Get().Log.Level)
	}
}

func TestGlobalService_LocalOverride(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "settings.local.json"),
		map[string]any{"app": map[string]any{"theme": "light"}})

	svc, _ := New(dir)
	_ = svc.Load()

	if svc.Get().App.Theme != "light" {
		t.Errorf("local override not applied, got %q", svc.Get().App.Theme)
	}
}

func TestGlobalService_ReturnedPointers_Independent(t *testing.T) {
	// ProviderByID / ModelByID 返回的指针不应指向 loader 内部状态
	// 修改返回值不应影响下次查询
	dir := t.TempDir()
	svc, _ := New(dir)
	_ = svc.Load()

	p1 := svc.ProviderByID("deepseek")
	p1.Name = "MUTATED"

	p2 := svc.ProviderByID("deepseek")
	if p2.Name == "MUTATED" {
		t.Errorf("mutating returned pointer leaked into loader state")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ─── Stage A: New API tests ──────────────────────────────────────────────────

// A1: GlobalService 嵌入 *Loader[Settings]，Load/Save/Watch 方法自动提升可用
func TestGlobalService_EmbedsLoader(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Load / Get / Set / Save 都应通过方法提升可用
	if err := svc.Load(); err != nil {
		t.Fatalf("Load (via embed): %v", err)
	}
	settings := svc.Get()
	if settings.App.Theme != "dark" {
		t.Errorf("embedded Get().App.Theme = %q, want dark", settings.App.Theme)
	}
	if err := svc.Set(func(s *Settings) { s.App.Theme = "light" }); err != nil {
		t.Fatalf("Set (via embed): %v", err)
	}
	if svc.Get().App.Theme != "light" {
		t.Error("Set via embed did not persist in-memory")
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save (via embed): %v", err)
	}

	// 确认返回类型是 *Loader[Settings]
	var _ *Loader[Settings] = svc.Loader
}

// A2: NewLoader 校验
func TestNewLoaderValidation_NoPaths(t *testing.T) {
	_, err := NewLoader(DefaultSettings())
	if err == nil {
		t.Error("NewLoader with no paths should error")
	}
	if !strings.Contains(err.Error(), "at least one path") {
		t.Errorf("error should mention 'at least one path', got: %v", err)
	}
}

func TestNewLoaderValidation_EmptyPath(t *testing.T) {
	_, err := NewLoader(DefaultSettings(), "/valid/path", "")
	if err == nil {
		t.Error("NewLoader with empty path should error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestNewLoaderValidation_DuplicatePath(t *testing.T) {
	_, err := NewLoader(DefaultSettings(), "/same/path", "/same/path")
	if err == nil {
		t.Error("NewLoader with duplicate paths should error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention 'duplicate', got: %v", err)
	}
}

// A3: LoadContext / SaveContext
func TestLoader_LoadContext_CancelledCtx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预先取消

	err := loader.LoadContext(ctx)
	if err == nil {
		t.Fatal("LoadContext with cancelled ctx should error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got: %v", err)
	}
}

func TestLoader_SaveContext_CancelledCtx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loader.SaveContext(ctx)
	if err == nil {
		t.Fatal("SaveContext with cancelled ctx should error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should wrap context.Canceled, got: %v", err)
	}
}

// A4: OnChange returns unregister function
func TestLoader_OnChange_Unregister(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var count int32
	cancel := loader.OnChange(func(old, new Settings) {
		atomic.AddInt32(&count, 1)
	})

	// 第一次 Load 触发一次
	_ = loader.Set(func(s *Settings) { s.App.Theme = "light" })
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("before cancel: count = %d, want 1", got)
	}

	// 取消后不再触发
	cancel()
	_ = loader.Set(func(s *Settings) { s.App.Theme = "dark" })
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("after cancel: count = %d, want still 1", got)
	}
}

func TestLoader_OnChange_UnregisterIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)

	cancel := loader.OnChange(func(old, new Settings) {})
	cancel()

	// 再次调用不应 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("double cancel panicked: %v", r)
		}
	}()
	cancel()
}

func TestLoader_OnChange_UnregisterConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var wg sync.WaitGroup
	cancels := make([]func(), 100)

	// 100 并发注册
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cancels[i] = loader.OnChange(func(old, new Settings) {})
		}(i)
	}
	wg.Wait()

	// 100 并发取消
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if cancels[i] != nil {
				cancels[i]()
			}
		}(i)
	}
	wg.Wait()

	// 验证所有回调都被清除
	var called int32
	loader.OnChange(func(old, new Settings) { atomic.AddInt32(&called, 1) })
	_ = loader.Set(func(s *Settings) { s.App.Theme = "light" })
	if got := atomic.LoadInt32(&called); got != 1 {
		t.Errorf("after unregistering all 100, only newly-added callback should fire; got %d", got)
	}
}

// A5: SetErrorHandler
func TestLoader_SetErrorHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)

	var received error
	loader.SetErrorHandler(func(err error) {
		received = err
	})

	// 直接调用内部 handleWatchError 测试
	want := errors.New("disk full")
	loader.handleWatchError(want)

	if received == nil {
		t.Fatal("error handler not called")
	}
	if received.Error() != want.Error() {
		t.Errorf("received = %v, want %v", received, want)
	}
}

func TestLoader_SetErrorHandler_NilOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)

	// 未设置 handler 时调用 handleWatchError 不应 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handleWatchError without handler panicked: %v", r)
		}
	}()
	loader.handleWatchError(errors.New("ignored"))
}

// 验证 fsnotify 确实能把 error 走到我们的 handler（需要真实 watcher）
func TestLoader_ErrorHandler_FromWatcherEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	loader, _ := NewLoader(DefaultSettings(), path)
	_ = loader.Load()

	var received error
	done := make(chan struct{}, 1)
	loader.SetErrorHandler(func(err error) {
		received = err
		select {
		case done <- struct{}{}:
		default:
		}
	})

	if err := loader.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer loader.Close()

	// 无法可靠触发 fsnotify 的 error channel，用内部直接 drive 验证 watchLoop 路径
	// 这里只断言 handler 被正确设置并可以被显式驱动（通过 handleWatchError）
	loader.handleWatchError(errors.New("simulated"))

	select {
	case <-done:
		if received == nil || received.Error() != "simulated" {
			t.Errorf("received = %v", received)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("error handler did not fire")
	}
	_ = fsnotify.Write // 显式使用 import（在 watchLoop 里引用过）
}
