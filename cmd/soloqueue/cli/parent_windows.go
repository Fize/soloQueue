//go:build windows

package cli

import (
	"syscall"
)

// isParentDead checks if the parent process has died on Windows.
func isParentDead(initialPPID int) bool {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	const STILL_ACTIVE = 259

	handle, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(initialPPID))
	if err != nil {
		// Process handle cannot be opened (likely dead)
		return true
	}
	defer syscall.CloseHandle(handle)

	var code uint32
	err = syscall.GetExitCodeProcess(handle, &code)
	if err != nil {
		return true
	}
	return code != STILL_ACTIVE
}
