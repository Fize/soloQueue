// Package managed prepares verified artifacts without requiring Python at runtime.
package managed

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

const Revision = "614241f622f53c4eeff9890bdc4f31cfecc418b3"
const ModelHash = "ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665"
const TokenizerHash = "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39"
const RuntimeVersion = "1.24.2"

type Paths struct{ ModelDir, Runtime, Worker string }
type Progress func(state string, downloaded, total int64)
type Installer struct {
	WorkDir      string
	Client       *http.Client
	GOOS, GOARCH string
	Worker       string
}
type artifact struct{ name, hash, url string }

func (i Installer) Prepare(ctx context.Context, progress Progress) (Paths, error) {
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	worker := i.Worker
	if worker == "" {
		worker = os.Getenv("SOLOQUEUE_ONNX_WORKER")
	}
	if worker == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, err
		}
		name := "soloqueue-embedding"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		worker = filepath.Join(filepath.Dir(exe), name)
	}
	worker, err := filepath.Abs(worker)
	if err != nil {
		return Paths{}, err
	}
	info, err := os.Stat(worker)
	if err != nil || info.IsDir() {
		return Paths{}, errors.New("local embedding worker missing; install the release package or set SOLOQUEUE_ONNX_WORKER")
	}
	osname, arch := i.GOOS, i.GOARCH
	if osname == "" {
		osname = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	packages, library, err := runtimeArtifacts(osname, arch)
	if err != nil {
		return Paths{}, err
	}
	modelDir := filepath.Join(i.WorkDir, "models", "embedding", "multilingual-e5-small", Revision)
	base := "https://huggingface.co/intfloat/multilingual-e5-small/resolve/" + Revision + "/"
	for _, a := range []artifact{{"onnx/model.onnx", ModelHash, base + "onnx/model.onnx"}, {"onnx/tokenizer.json", TokenizerHash, base + "onnx/tokenizer.json"}} {
		if err := i.download(ctx, filepath.Join(modelDir, filepath.FromSlash(a.name)), a, progress); err != nil {
			return Paths{}, err
		}
	}
	root := filepath.Join(i.WorkDir, "runtimes", "embedding", RuntimeVersion, osname+"-"+arch)
	for _, a := range packages {
		archive := filepath.Join(root, "archives", a.name)
		if err := i.download(ctx, archive, a, progress); err != nil {
			return Paths{}, err
		}
		progress("preparing", 0, 0)
		if err := extract(ctx, archive, root); err != nil {
			return Paths{}, fmt.Errorf("extract local runtime: %w", err)
		}
	}
	runtimePath := filepath.Join(root, filepath.FromSlash(library))
	if stat, err := os.Stat(runtimePath); err != nil || stat.IsDir() {
		return Paths{}, errors.New("verified runtime archive is missing its native library")
	}
	return Paths{ModelDir: modelDir, Runtime: runtimePath, Worker: worker}, nil
}
func runtimeArtifacts(osname, arch string) ([]artifact, string, error) {
	github := "https://github.com/microsoft/onnxruntime/releases/download/v1.24.2/"
	switch osname + "/" + arch {
	case "darwin/arm64":
		name := "onnxruntime-osx-arm64-1.24.2.tgz"
		return []artifact{{name, "0af4fa503e8ea285245b47ee42d0a7461b8156a81270857da0c1d4ecf858abde", github + name}}, "onnxruntime-osx-arm64-1.24.2/lib/libonnxruntime.1.24.2.dylib", nil
	case "linux/amd64":
		name := "onnxruntime-linux-x64-gpu-1.24.2.tgz"
		return []artifact{{name, "bcb42da041f42192e5579de175f7410313c114740a611e230afe9d79be65cc49", github + name}}, "onnxruntime-linux-x64-gpu-1.24.2/lib/libonnxruntime.so.1.24.2", nil
	case "linux/arm64":
		name := "onnxruntime-linux-aarch64-1.24.2.tgz"
		return []artifact{{name, "6715b3d19965a2a6981e78ed4ba24f17a8c30d2d26420dbed10aac7ceca0085e", github + name}}, "onnxruntime-linux-aarch64-1.24.2/lib/libonnxruntime.so.1.24.2", nil
	case "windows/amd64":
		return []artifact{
			{"microsoft.ml.onnxruntime.directml.1.24.2.nupkg", "c9b8adb96dfb5578097bea42a7d9b7ff8f300fb3c3a6f3052fe5b702628ab681", "https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/1.24.2/microsoft.ml.onnxruntime.directml.1.24.2.nupkg"},
			{"microsoft.ai.directml.1.15.4.nupkg", "4e7cb7ddce8cf837a7a75dc029209b520ca0101470fcdf275c1f49736a3615b9", "https://api.nuget.org/v3-flatcontainer/microsoft.ai.directml/1.15.4/microsoft.ai.directml.1.15.4.nupkg"}}, "runtimes/win-x64/native/onnxruntime.dll", nil
	default:
		return nil, "", fmt.Errorf("local ONNX runtime unsupported on %s/%s; supported: macOS arm64, Linux amd64, Windows amd64", osname, arch)
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
func (i Installer) download(ctx context.Context, path string, a artifact, progress Progress) error {
	if verified(ctx, path, a.hash) {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	client := i.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s failed: %w", a.name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", a.name, response.StatusCode)
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
	if hex.EncodeToString(h.Sum(nil)) != a.hash {
		return fmt.Errorf("download %s checksum mismatch", a.name)
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
