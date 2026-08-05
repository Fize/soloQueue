// Package delivery applies only the explicit delivery actions attached to a
// workflow task. The workflow graph itself never commits, pushes, or creates
// pull requests.
package delivery

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/workflow/worktree"
)

const DefaultTimeout = 15 * time.Minute

type Request struct {
	Commit      *CommitRequest      `json:"commit,omitempty"`
	Push        *PushRequest        `json:"push,omitempty"`
	PullRequest *PullRequestRequest `json:"pull_request,omitempty"`
}

type CommitRequest struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message,omitempty"`
}

type PushRequest struct {
	Enabled bool   `json:"enabled"`
	Remote  string `json:"remote,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

type PullRequestRequest struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body,omitempty"`
	Draft   bool   `json:"draft,omitempty"`
}

type Result struct {
	Status      string `json:"status"`
	CommitHash  string `json:"commit_hash,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Branch      string `json:"branch,omitempty"`
	PullRequest string `json:"pull_request,omitempty"`
	Error       string `json:"error,omitempty"`
}

func Execute(ctx context.Context, record worktree.Record, request Request) Result {
	if !enabled(request) {
		return Result{Status: "not_requested"}
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	if request.PullRequest != nil && request.PullRequest.Enabled && (request.Commit == nil || !request.Commit.Enabled || request.Push == nil || !request.Push.Enabled) {
		return Result{Status: "blocked", Error: "pull request requires explicit commit and push"}
	}
	result := Result{Status: "completed"}
	if request.Commit != nil && request.Commit.Enabled {
		message := strings.TrimSpace(request.Commit.Message)
		if message == "" {
			message = "workflow: apply requested changes"
		}
		if err := run(ctx, record.Path, "add", "-A"); err != nil {
			return failed(result, err)
		}
		if err := run(ctx, record.Path, "commit", "-m", message); err != nil {
			return failed(result, err)
		}
		hash, err := output(ctx, record.Path, "git", "rev-parse", "HEAD")
		if err != nil {
			return failed(result, err)
		}
		result.CommitHash = hash
	}
	if request.Push != nil && request.Push.Enabled {
		remote := strings.TrimSpace(request.Push.Remote)
		if remote == "" {
			remote = "origin"
		}
		branch := strings.TrimSpace(request.Push.Branch)
		if branch == "" {
			branch = record.Branch
		}
		if err := run(ctx, record.Path, "push", remote, "HEAD:"+branch); err != nil {
			return failed(result, err)
		}
		result.Remote, result.Branch = remote, branch
	}
	if request.PullRequest != nil && request.PullRequest.Enabled {
		args := []string{"pr", "create", "--title", request.PullRequest.Title, "--body", request.PullRequest.Body}
		if request.PullRequest.Draft {
			args = append(args, "--draft")
		}
		url, err := output(ctx, record.Path, "gh", args...)
		if err != nil {
			return failed(result, err)
		}
		result.PullRequest = url
	}
	return result
}

func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

func enabled(request Request) bool {
	return (request.Commit != nil && request.Commit.Enabled) || (request.Push != nil && request.Push.Enabled) || (request.PullRequest != nil && request.PullRequest.Enabled)
}

func failed(result Result, err error) Result {
	result.Status = "failed"
	result.Error = err.Error()
	return result
}

func run(ctx context.Context, dir string, args ...string) error {
	_, err := output(ctx, dir, "git", args...)
	return err
}

func output(ctx context.Context, dir, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("delivery %s: %s: %w", command, strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}
