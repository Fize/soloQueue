//go:build windows

package tools

import "os/exec"

func configureRuntimeProcess(_ *exec.Cmd) {}

func killRuntimeProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
