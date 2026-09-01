package cli

import "os"

// isParentDead reports whether the parent process has exited. Orphaned
// processes are re-parented to init (PID 1) on supported Unix-like systems.
func isParentDead(initialPPID int) bool {
	return os.Getppid() == 1
}
