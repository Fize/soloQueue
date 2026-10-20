//go:build !windows

package server

import (
	"os"
	"syscall"
)

func openChangeFileNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
