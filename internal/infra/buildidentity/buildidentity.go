// Package buildidentity identifies the executable that started the current process.
package buildidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Capture at process startup, before an update can replace the executable's path.
var current, currentErr = executableIdentity()

func executableIdentity() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return File(path)
}

// Current returns the identity captured when this process started.
func Current() (string, error) { return current, currentErr }

// File computes the identity of an executable on disk.
func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open executable for identity: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read executable for identity: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
