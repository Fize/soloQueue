// Package speechinstall installs pinned speech tools into versioned directories.
package speechinstall

import (
	"archive/zip"
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
const maxExtractBytes = 512 << 20

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
	if !bytesEqual(hash.Sum(nil), expected) {
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
	names := []string{}
	var total uint64
	seen := map[string]bool{}
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
		key := strings.ToLower(name)
		if seen[key] || !entry.Mode().IsRegular() {
			return "", fmt.Errorf("invalid duplicate or non-regular archive entry %q", entry.Name)
		}
		seen[key] = true
		if entry.UncompressedSize64 > maxExtractBytes || total > maxExtractBytes-entry.UncompressedSize64 {
			return "", fmt.Errorf("speech archive exceeds extraction limit")
		}
		total += entry.UncompressedSize64
		source, err := entry.Open()
		if err != nil {
			return "", err
		}
		dest, err := os.OpenFile(filepath.Join(output, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			source.Close()
			return "", err
		}
		_, copyErr := io.Copy(dest, io.LimitReader(source, int64(entry.UncompressedSize64)+1))
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
		names = append(names, name)
		if name == spec.Executable {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("speech executable missing from verified archive")
	}
	if err := WriteManifest(output, names); err != nil {
		return "", err
	}
	destination := filepath.Join(root, spec.Directory)
	if err := replaceDirectory(output, destination); err != nil {
		return "", fmt.Errorf("install speech archive: %w", err)
	}
	return filepath.Join(destination, spec.Executable), nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
