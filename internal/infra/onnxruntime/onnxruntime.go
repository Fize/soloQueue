// Package onnxruntime provisions pinned ONNX Runtime builds without requiring a system install.
//
// Two versions are in use and they are not interchangeable: the local embedding worker is built
// against API 24 (runtime 1.24.2) while the decision worker's `ort` binding refuses anything below
// 1.29 and is pinned to 1.30.0. Each consumer passes the version it needs; artifacts land under
// <workDir>/runtimes/<namespace>/<version>/<goos-goarch>/ so the two never collide.
package onnxruntime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Progress reports a coarse state plus byte counters while artifacts are fetched.
type Progress func(state string, downloaded, total int64)

// Config selects the runtime build to provision. Namespace keeps each consumer's cache directory
// (and therefore its on-disk migration path) unchanged: embedding uses "embedding", the local
// decision worker uses "decide".
type Config struct {
	WorkDir      string
	Namespace    string
	Version      string
	GOOS, GOARCH string
	Client       *http.Client
}

type artifact struct{ name, hash, url string }

// Ensure downloads, verifies and extracts the pinned runtime for cfg, returning the absolute path
// of its shared library.
func Ensure(ctx context.Context, cfg Config, progress Progress) (string, error) {
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	osname, arch := cfg.GOOS, cfg.GOARCH
	if osname == "" {
		osname = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	packages, library, err := artifacts(cfg.Version, osname, arch)
	if err != nil {
		return "", err
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "onnxruntime"
	}
	root := filepath.Join(cfg.WorkDir, "runtimes", namespace, cfg.Version, osname+"-"+arch)
	for _, a := range packages {
		archive := filepath.Join(root, "archives", a.name)
		if err := Download(ctx, cfg.Client, archive, a.name, a.url, a.hash, progress); err != nil {
			return "", err
		}
		progress("preparing", 0, 0)
		if err := extract(ctx, archive, root); err != nil {
			return "", fmt.Errorf("extract local runtime: %w", err)
		}
	}
	runtimePath := filepath.Join(root, filepath.FromSlash(library))
	if stat, err := os.Stat(runtimePath); err != nil || stat.IsDir() {
		return "", errors.New("verified runtime archive is missing its native library")
	}
	return runtimePath, nil
}

// Download fetches url into path, verifying sha256Hex. An already-verified file is reused without
// touching the network; a corrupt cache entry is only replaced once its verified replacement is
// complete.
func Download(ctx context.Context, client *http.Client, path, name, url, sha256Hex string, progress Progress) error {
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	if verified(ctx, path, sha256Hex) {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s failed: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", name, response.StatusCode)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	h := sha256.New()
	var done int64
	total := response.ContentLength
	if total < 0 {
		total = 0
	}
	progress("downloading", 0, total)
	buffer := make([]byte, 256*1024)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, err := io.MultiWriter(temp, h).Write(buffer[:n]); err != nil {
				return err
			}
			done += int64(n)
			progress("downloading", done, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != sha256Hex {
		return fmt.Errorf("download %s checksum mismatch", name)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// Remove a corrupted cache entry only after its verified replacement is complete.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func artifacts(version, osname, arch string) ([]artifact, string, error) {
	github := "https://github.com/microsoft/onnxruntime/releases/download/v" + version + "/"
	switch version + "/" + osname + "/" + arch {
	case "1.24.2/darwin/arm64":
		name := "onnxruntime-osx-arm64-1.24.2.tgz"
		return []artifact{{name, "0af4fa503e8ea285245b47ee42d0a7461b8156a81270857da0c1d4ecf858abde", github + name}},
			"onnxruntime-osx-arm64-1.24.2/lib/libonnxruntime.1.24.2.dylib", nil
	case "1.24.2/linux/amd64":
		name := "onnxruntime-linux-x64-gpu-1.24.2.tgz"
		return []artifact{{name, "bcb42da041f42192e5579de175f7410313c114740a611e230afe9d79be65cc49", github + name}},
			"onnxruntime-linux-x64-gpu-1.24.2/lib/libonnxruntime.so.1.24.2", nil
	case "1.24.2/linux/arm64":
		name := "onnxruntime-linux-aarch64-1.24.2.tgz"
		return []artifact{{name, "6715b3d19965a2a6981e78ed4ba24f17a8c30d2d26420dbed10aac7ceca0085e", github + name}},
			"onnxruntime-linux-aarch64-1.24.2/lib/libonnxruntime.so.1.24.2", nil
	case "1.24.2/windows/amd64":
		return []artifact{
			{"microsoft.ml.onnxruntime.directml.1.24.2.nupkg", "c9b8adb96dfb5578097bea42a7d9b7ff8f300fb3c3a6f3052fe5b702628ab681", "https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/1.24.2/microsoft.ml.onnxruntime.directml.1.24.2.nupkg"},
			{"microsoft.ai.directml.1.15.4.nupkg", "4e7cb7ddce8cf837a7a75dc029209b520ca0101470fcdf275c1f49736a3615b9", "https://api.nuget.org/v3-flatcontainer/microsoft.ai.directml/1.15.4/microsoft.ai.directml.1.15.4.nupkg"},
		}, "runtimes/win-x64/native/onnxruntime.dll", nil
	case "1.30.0/darwin/arm64":
		name := "onnxruntime-osx-arm64-1.30.0.tgz"
		return []artifact{{name, "6ebb5062a934537c352937821f9fe9718e7de1a2db1122a93dd363ffd53a7012", github + name}},
			"onnxruntime-osx-arm64-1.30.0/lib/libonnxruntime.1.30.0.dylib", nil
	case "1.30.0/linux/amd64":
		name := "onnxruntime-linux-x64-1.30.0.tgz"
		return []artifact{{name, "a5ed5a3cac51fbb2e90da632ae43d19212faaa20e76484e62bcb7c23ddb3b3fd", github + name}},
			"onnxruntime-linux-x64-1.30.0/lib/libonnxruntime.so.1.30.0", nil
	case "1.30.0/linux/arm64":
		name := "onnxruntime-linux-aarch64-1.30.0.tgz"
		return []artifact{{name, "e16a27a8ed330bbc698df7330b0cf56e722f354e3bcc92118682c74ef3c3e3da", github + name}},
			"onnxruntime-linux-aarch64-1.30.0/lib/libonnxruntime.so.1.30.0", nil
	case "1.30.0/windows/amd64":
		return []artifact{
			{"microsoft.ml.onnxruntime.1.30.0.nupkg", "30df119bd9164eaa187666d93147448fdd4a9a1a3caf83f1505daf7af2d1c5d2", "https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime/1.30.0/microsoft.ml.onnxruntime.1.30.0.nupkg"},
		}, "runtimes/win-x64/native/onnxruntime.dll", nil
	default:
		return nil, "", fmt.Errorf("ONNX Runtime %s unsupported on %s/%s; supported: 1.24.2 and 1.30.0 on macOS arm64, Linux amd64/arm64, Windows amd64", version, osname, arch)
	}
}

func verified(ctx context.Context, path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, &contextReader{ctx: ctx, reader: f})
	return err == nil && hex.EncodeToString(h.Sum(nil)) == want
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func safePath(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", errors.New("unsafe archive path")
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("unsafe archive path")
	}
	return path, nil
}

func writeMember(ctx context.Context, root, name string, reader io.Reader, mode os.FileMode) error {
	path, err := safePath(root, name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".extract-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = io.Copy(file, &contextReader{ctx: ctx, reader: reader}); err != nil {
		return err
	}
	if err = file.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(file.Name(), path)
}

func extract(ctx context.Context, path, root string) error {
	if strings.HasSuffix(path, ".nupkg") || strings.HasSuffix(path, ".zip") {
		archive, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer archive.Close()
		for _, member := range archive.File {
			if _, err := safePath(root, member.Name); err != nil {
				return err
			}
			if member.FileInfo().IsDir() {
				continue
			}
			name := member.Name
			// DirectML's redistributable must be beside onnxruntime.dll.
			if strings.HasSuffix(path, "microsoft.ai.directml.1.15.4.nupkg") {
				if name != "bin/x64-win/DirectML.dll" {
					continue
				}
				name = "runtimes/win-x64/native/DirectML.dll"
			} else if !strings.HasPrefix(name, "runtimes/win-x64/native/") || !strings.HasSuffix(strings.ToLower(name), ".dll") {
				continue
			}
			source, err := member.Open()
			if err != nil {
				return err
			}
			err = writeMember(ctx, root, name, source, 0600)
			source.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	for {
		member, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := safePath(root, member.Name); err != nil {
			return err
		}
		switch member.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			if err := writeMember(ctx, root, member.Name, archive, os.FileMode(member.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Versioned runtime and providers are real files; linker aliases are unnecessary.
			if _, err := safePath(filepath.Dir(filepath.Join(root, member.Name)), member.Linkname); err != nil {
				return err
			}
		default:
			return errors.New("unsupported runtime archive member")
		}
	}
}
