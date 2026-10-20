//go:build !windows

package skill

import (
	"os"
	"syscall"
)

func openSkillFileNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
