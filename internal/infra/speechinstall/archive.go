// Package speechinstall installs pinned speech tools into versioned directories.
package speechinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxArchiveBytes = 256 << 20

type Archive struct {
	URL, SHA256, Directory, Prefix, Executable string
}

// InstallArchive verifies an archive before extracting the executable and DLLs.
// A versioned destination keeps an existing installation intact on failure.
func InstallArchive(ctx context.Context, client *http.Client, spec Archive, root string) (string, error) {
	if spec.Directory == "" || spec.Directory == "." || spec.Directory == ".." || filepath.Base(spec.Directory) != spec.Directory || spec.Executable == "" || filepath.Base(spec.Executable) != spec.Executable {
		return "", fmt.Errorf("invalid archive destination")
	}
	expected, err := hex.DecodeString(spec.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return "", fmt.Errorf("invalid archive checksum")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(root, ".speech-install-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	archivePath := filepath.Join(stage, "archive.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxArchiveBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > maxArchiveBytes {
		return "", fmt.Errorf("speech archive exceeds size limit")
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return "", fmt.Errorf("speech archive checksum mismatch")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	output := filepath.Join(stage, "output")
	if err := os.Mkdir(output, 0o755); err != nil {
		return "", err
	}
	found := false
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name, spec.Prefix) {
			continue
		}
		name := strings.TrimPrefix(entry.Name, spec.Prefix)
		if name == "" || name != path.Base(name) || strings.ContainsAny(name, `\\:`) {
			return "", fmt.Errorf("invalid speech archive entry %q", entry.Name)
		}
		if name != spec.Executable && !strings.HasSuffix(strings.ToLower(name), ".dll") {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return "", err
		}
		dest, err := os.OpenFile(filepath.Join(output, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			source.Close()
			return "", err
		}
		_, copyErr := io.Copy(dest, source)
		sourceErr := source.Close()
		destErr := dest.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if sourceErr != nil {
			return "", sourceErr
		}
		if destErr != nil {
			return "", destErr
		}
		if name == spec.Executable {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("speech executable missing from verified archive")
	}
	destination := filepath.Join(root, spec.Directory)
	if err := replaceDirectory(output, destination); err != nil {
		return "", fmt.Errorf("install speech archive: %w", err)
	}
	return filepath.Join(destination, spec.Executable), nil
}
