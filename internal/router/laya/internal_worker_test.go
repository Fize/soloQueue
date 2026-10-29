package laya

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/buildidentity"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func testWorkerBuildID(t *testing.T, binary string) string {
	t.Helper()
	id, err := buildidentity.File(binary)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestInternalWorkerRealModel(t *testing.T) {
	binary, model, runtime := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY"), os.Getenv("SOLOQUEUE_TEST_LAYA_MODEL"), os.Getenv("SOLOQUEUE_TEST_LAYA_RUNTIME")
	if binary == "" || model == "" || runtime == "" {
		t.Skip("main binary, model and runtime not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	model, err = filepath.Rel(cwd, model)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = filepath.Rel(cwd, runtime)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := startWorker(ctx, binary, model, runtime, true, testWorkerBuildID(t, binary))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close()
	label, err := worker.decide(ctx, `[{"role":"user","content":"Please inspect this issue"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if v, e := tasktype.Parse(label); e != nil || !v.Valid() {
		t.Fatalf("invalid task type %q", label)
	}
	large, err := json.Marshal([]map[string]string{{"role": "user", "content": strings.Repeat(" ", 70<<10) + "Please inspect this issue"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.decide(ctx, string(large)); err != nil {
		t.Fatalf("valid message larger than 64 KiB failed: %v", err)
	}
	if _, err = worker.decide(ctx, "invalid messages"); !errors.Is(err, errInvalidRequest) {
		t.Fatalf("invalid input: %v", err)
	}
	if _, err = worker.decide(ctx, `[{"role":"user","content":"Please inspect this issue"}]`); err != nil {
		t.Fatalf("worker did not recover after rejected input: %v", err)
	}
	dir := worker.workDir
	if dir == "" {
		t.Fatal("missing isolated worker directory")
	}
	_ = worker.close()
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worker directory not removed: %v", err)
	}
}

func TestInternalWorkerRejectsDifferentBuild(t *testing.T) {
	binary := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY")
	if binary == "" {
		t.Skip("main binary not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if worker, err := startWorker(ctx, binary, t.TempDir(), filepath.Join(t.TempDir(), "runtime"), true, "different-build"); err == nil {
		_ = worker.close()
		t.Fatal("different build accepted")
	} else if !strings.Contains(err.Error(), "incompatible internal classifier build") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInternalWorkerProvisioned(t *testing.T) {
	binary, workDir := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY"), os.Getenv("SOLOQUEUE_LAYA_TEST_WORKDIR")
	if binary == "" || workDir == "" {
		t.Skip("main binary and provisioning work directory not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	manager := New(workDir, nil)
	defer manager.Close()
	model, err := manager.prepareModel(ctx, 0, Config{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := manager.runtimePath(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := startWorker(ctx, binary, model, runtime, true, testWorkerBuildID(t, binary))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close()
	label, err := worker.decide(ctx, `[{"role":"user","content":"Please inspect this issue"}]`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tasktype.Parse(label)
	if err != nil || !result.Valid() {
		t.Fatalf("invalid task type %q", label)
	}
}

func TestInternalClassifierRealConversation(t *testing.T) {
	binary, model, runtime, text := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY"), os.Getenv("SOLOQUEUE_TEST_LAYA_MODEL"), os.Getenv("SOLOQUEUE_TEST_LAYA_RUNTIME"), os.Getenv("SOLOQUEUE_TEST_LAYA_CASE")
	if binary == "" || model == "" || runtime == "" || text == "" {
		t.Skip("main binary, model, runtime and conversation not configured")
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	manager := New(t.TempDir(), nil)
	defer manager.Close()
	manager.workerPath = func() (string, error) { return binary, nil }
	manager.prepareFn = func(context.Context, Config) (string, error) { return model, nil }
	manager.spawnFn = func(ctx context.Context, path, dir, _ string) (proc, error) {
		return startWorker(ctx, path, dir, runtime, true, testWorkerBuildID(t, path))
	}
	manager.Apply(Config{Enabled: true})
	deadline := time.Now().Add(time.Minute)
	for manager.Status().State != "ready" {
		if status := manager.Status(); status.State == "error" {
			t.Fatalf("laya failed: %s", status.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("laya startup timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	classifier := router.NewDefaultClassifier(router.ClassifierConfig{Engine: router.EngineLaya}, nil, "", "", manager, nil)
	result := classifier.Classify(context.Background(), router.ClassifyInput{Text: text}, nil)
	if result.Source != router.SourceLaya || !result.TaskType.Valid() {
		t.Fatalf("classification did not use laya: %+v", result)
	}
	longMessage, err := json.Marshal([]map[string]string{{"role": "user", "content": strings.Repeat(" ", 70<<10) + text}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Decide(context.Background(), string(longMessage)); err != nil {
		t.Fatalf("valid message larger than 64 KiB failed: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err = manager.Decide(context.Background(), strings.Repeat("x", 1<<20)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("oversized input: %v", err)
		}
		if _, err = manager.Decide(context.Background(), "invalid messages"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid input: %v", err)
		}
	}
	if manager.Status().State != "ready" {
		t.Fatalf("rejected input affected the worker: %+v", manager.Status())
	}
	if next := classifier.Classify(context.Background(), router.ClassifyInput{Text: text}, nil); next.Source != router.SourceLaya {
		t.Fatalf("worker did not recover after rejected input: %+v", next)
	}
	manager.Close()
	fallback := classifier.Classify(context.Background(), router.ClassifyInput{Text: text, PreviousTaskType: tasktype.Engineering}, nil)
	if fallback.Source != router.SourceLayaUnavailable || fallback.TaskType != tasktype.Engineering {
		t.Fatalf("fallback mismatch: %+v", fallback)
	}
}
