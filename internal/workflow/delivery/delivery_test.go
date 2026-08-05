package delivery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/workflow/worktree"
)

func TestBoundedContextAddsDefaultDeadline(t *testing.T) {
	ctx, cancel := boundedContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected delivery context deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > DefaultTimeout {
		t.Fatalf("unexpected remaining duration: %s", remaining)
	}
}

func TestExecuteDoesNothingUnlessRequested(t *testing.T) {
	result := Execute(context.Background(), worktree.Record{Path: t.TempDir()}, Request{})
	if result.Status != "not_requested" {
		t.Fatalf("status=%q", result.Status)
	}
}

func TestExecuteExplicitCommitOnly(t *testing.T) {
	repo := t.TempDir()
	gitDeliveryTest(t, repo, "init", "-q")
	gitDeliveryTest(t, repo, "config", "user.email", "workflow@example.invalid")
	gitDeliveryTest(t, repo, "config", "user.name", "Workflow")
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Execute(context.Background(), worktree.Record{Path: repo, Branch: "codex/test"}, Request{Commit: &CommitRequest{Enabled: true, Message: "requested"}})
	if result.Status != "completed" || result.CommitHash == "" {
		t.Fatalf("result=%+v", result)
	}
	if strings.TrimSpace(gitOutput(t, repo, "status", "--porcelain")) != "" {
		t.Fatal("expected clean checkout after commit")
	}
}

func gitDeliveryTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
