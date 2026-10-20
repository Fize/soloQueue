package tools

import (
	"errors"
	"os"
	"os/exec"
)

// Windows has no Unix process groups. Keep cancellation available for the
// launched process without invoking an external shell or taskkill command.
func configureRuntimeProcess(_ *exec.Cmd) {}

func killRuntimeProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
