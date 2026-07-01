//go:build !windows

package cli

import (
	"os"
)

// isParentDead checks if the parent process has died.
// On Unix, if the parent process dies, we get re-parented to init (PID 1).
func isParentDead(initialPPID int) bool {
	return os.Getppid() == 1
}
