//go:build !windows

package process

import (
	"context"
	"os/exec"
)

func Command(ctx context.Context, name string, args []string, _ string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, name, args...), nil
}
