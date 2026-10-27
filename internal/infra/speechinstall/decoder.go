package speechinstall

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const DecoderDirectory = "silk-windows"

// InstallDecoder copies the decoder shipped alongside the release binary.
func InstallDecoder(sourcePath, root string) (string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open bundled SILK decoder: %w", err)
	}
	defer source.Close()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(root, ".silk-install-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	output := filepath.Join(stage, "silk-decoder.exe")
	dest, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(dest, source)
	closeErr := dest.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	target := filepath.Join(root, DecoderDirectory)
	if err := replaceDirectory(stage, target); err != nil {
		return "", fmt.Errorf("install bundled SILK decoder: %w", err)
	}
	return filepath.Join(target, "silk-decoder.exe"), nil
}
