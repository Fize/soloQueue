// Package worktree owns the isolated Git workspaces used by durable workflow
// runs. A run never writes to the caller's checkout directly.
package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/infra/workdir"
)

// Record is the durable identity of a workflow worktree.
type Record struct {
	RunID          string `json:"run_id"`
	RepositoryPath string `json:"repository_path"`
	Path           string `json:"path"`
	Branch         string `json:"branch"`
	BaseRef        string `json:"base_ref"`
	BaseCommit     string `json:"base_commit"`
	State          string `json:"state"`
}

// Manager creates and inspects worktrees. Cleanup is explicit; no operation
// in this package removes a worktree implicitly after a run finishes.
type Manager struct {
	Root string
}

func NewManager(root string) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("workflow_worktree_invalid: root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workflow_worktree_invalid: root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("workflow_worktree_invalid: mkdir: %w", err)
	}
	return &Manager{Root: root}, nil
}

// Prepare validates the repository and creates a new branch/worktree from
// baseRef. The repository checkout itself is never changed by this method.
func (m *Manager) Prepare(ctx context.Context, runID, repository, baseRef, branch string) (Record, error) {
	if m == nil {
		return Record{}, fmt.Errorf("workflow_worktree_invalid: manager is nil")
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(repository) == "" {
		return Record{}, fmt.Errorf("workflow_worktree_invalid: run_id and repository are required")
	}
	repo, err := workdir.NormalizeExistingDir(repository)
	if err != nil {
		return Record{}, fmt.Errorf("workflow_worktree_repository_invalid: %w", err)
	}
	if baseRef = strings.TrimSpace(baseRef); baseRef == "" {
		baseRef = "HEAD"
	}
	if branch = strings.TrimSpace(branch); branch == "" {
		branch = "soloqueue/workflow/" + runID
	}
	if _, err := runGit(ctx, repo, "rev-parse", "--show-toplevel"); err != nil {
		return Record{}, fmt.Errorf("workflow_worktree_repository_invalid: not a git repository: %w", err)
	}
	baseCommit, err := runGit(ctx, repo, "rev-parse", "--verify", baseRef+"^{commit}")
	if err != nil {
		return Record{}, fmt.Errorf("workflow_worktree_base_invalid: %s: %w", baseRef, err)
	}
	path := filepath.Join(m.Root, runID)
	if _, err := os.Stat(path); err == nil {
		return Record{}, fmt.Errorf("workflow_worktree_exists: %s", path)
	} else if !os.IsNotExist(err) {
		return Record{}, fmt.Errorf("workflow_worktree_path_invalid: %w", err)
	}
	if _, err := runGit(ctx, repo, "worktree", "add", "-b", branch, path, baseCommit); err != nil {
		return Record{}, fmt.Errorf("workflow_worktree_create_failed: %w", err)
	}
	return Record{RunID: runID, RepositoryPath: repo, Path: path, Branch: branch, BaseRef: baseRef, BaseCommit: baseCommit, State: "active"}, nil
}

// Inspect returns the current HEAD and a stable hash of porcelain status.
func (m *Manager) Inspect(ctx context.Context, record Record) (head, status, statusHash string, err error) {
	if strings.TrimSpace(record.Path) == "" {
		return "", "", "", fmt.Errorf("workflow_worktree_invalid: path is required")
	}
	head, err = runGit(ctx, record.Path, "rev-parse", "HEAD")
	if err != nil {
		return "", "lost", "", fmt.Errorf("workflow_worktree_inspect_head: %w", err)
	}
	status, err = runGit(ctx, record.Path, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return head, "unknown", "", fmt.Errorf("workflow_worktree_inspect_status: %w", err)
	}
	state := "clean"
	if strings.TrimSpace(status) != "" {
		state = "dirty"
	}
	digest := sha256.Sum256([]byte(status))
	return head, state, hex.EncodeToString(digest[:]), nil
}

// Remove explicitly removes a worktree. Callers should only invoke this from
// a user-requested cleanup action; force is required for dirty worktrees.
func (m *Manager) Remove(ctx context.Context, record Record, force bool) error {
	if strings.TrimSpace(record.Path) == "" || strings.TrimSpace(record.RepositoryPath) == "" {
		return fmt.Errorf("workflow_worktree_invalid: repository and path are required")
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, record.Path)
	if _, err := runGit(ctx, record.RepositoryPath, args...); err != nil {
		return fmt.Errorf("workflow_worktree_remove_failed: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := execCommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed == "" {
			trimmed = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), trimmed)
	}
	return trimmed, nil
}
