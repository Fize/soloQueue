package tools

import (
	"os/exec"

	"github.com/xiaobaitu/soloqueue/internal/infra/process"
)

func configureRuntimeProcess(cmd *exec.Cmd)   { process.Configure(cmd) }
func startRuntimeProcess(cmd *exec.Cmd) error { return process.Start(cmd) }
func killRuntimeProcess(cmd *exec.Cmd) error  { return process.Kill(cmd) }
func closeRuntimeProcess(cmd *exec.Cmd)       { process.Close(cmd) }

func cleanupAfterRuntimeProcessWait(cmd *exec.Cmd) error { return process.CleanupAfterWait(cmd) }
