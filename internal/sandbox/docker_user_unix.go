//go:build !windows

package sandbox

import (
	"fmt"
	"os"
)

func sandboxContainerUser() string {
	uid, gid := sandboxContainerIDs()
	return fmt.Sprintf("%d:%d", uid, gid)
}

func sandboxContainerIDs() (int, int) {
	return os.Getuid(), os.Getgid()
}
