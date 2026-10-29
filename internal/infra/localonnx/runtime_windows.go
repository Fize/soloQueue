//go:build windows && cgo

package localonnx

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// The process is dedicated to one model; adjacent provider DLLs belong to this runtime.
func configureRuntimeDirectory(path string) error { return windows.SetDllDirectory(filepath.Dir(path)) }
