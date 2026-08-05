package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareInspectAndRemove(t *testing.T) {
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	gitTestCommand(t, repo, "config", "user.email", "workflow-test@example.invalid")
	gitTestCommand(t, repo, "config", "user.name", "Workflow Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repo, "add", "README.md")
	gitTestCommand(t, repo, "commit", "-qm", "base")

	manager, err := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Prepare(context.Background(), "wf_test", repo, "HEAD", "codex/workflow/test")
	if err != nil {
		t.Fatal(err)
	}
	if record.BaseCommit == "" || record.Path == repo || !strings.HasPrefix(record.Branch, "codex/") {
		t.Fatalf("unexpected record: %+v", record)
	}
	if _, err := os.Stat(record.Path); err != nil {
		t.Fatal(err)
	}
	head, state, hash, err := manager.Inspect(context.Background(), record)
	if err != nil || head != record.BaseCommit || state != "clean" || hash == "" {
		t.Fatalf("inspect = head=%q state=%q hash=%q err=%v", head, state, hash, err)
	}
	if err := os.WriteFile(filepath.Join(record.Path, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = manager.Inspect(context.Background(), record)
	if err != nil || state != "dirty" {
		t.Fatalf("dirty inspect = state=%q err=%v", state, err)
	}
	if err := manager.Remove(context.Background(), record, true); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareUsesSoloQueueBranchPrefixByDefault(t *testing.T) {
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	gitTestCommand(t, repo, "config", "user.email", "workflow-test@example.invalid")
	gitTestCommand(t, repo, "config", "user.name", "Workflow Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repo, "add", "README.md")
	gitTestCommand(t, repo, "commit", "-qm", "base")
	manager, err := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Prepare(context.Background(), "wf_default", repo, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Remove(context.Background(), record, true) })
	if record.Branch != "soloqueue/workflow/wf_default" {
		t.Fatalf("branch = %q", record.Branch)
	}
}

func gitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
