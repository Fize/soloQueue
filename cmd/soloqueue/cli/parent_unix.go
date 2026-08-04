//go:build !windows

package cli

import (
	"os"
)

// isParentDead reports whether the parent process has exited.
// On Unix, orphaned processes are re-parented to init (PID 1).
// initialPPID is unused here but kept for signature parity with the Windows implementation.
func isParentDead(initialPPID int) bool {
	return os.Getppid() == 1
}
