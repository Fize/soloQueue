//go:build !windows

package worktree

import "os/exec"

var execCommandContext = exec.CommandContext
