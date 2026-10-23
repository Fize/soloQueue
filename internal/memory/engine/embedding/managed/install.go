// Package managed prepares verified artifacts without requiring Python at runtime.
package managed

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/xiaobaitu/soloqueue/internal/infra/onnxruntime"
)

const Revision = "614241f622f53c4eeff9890bdc4f31cfecc418b3"
const ModelHash = "ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665"
const TokenizerHash = "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39"
const RuntimeVersion = "1.24.2"

type Paths struct{ ModelDir, Runtime, Worker string }

// Progress reports a coarse state plus byte counters while artifacts are fetched.
type Progress = onnxruntime.Progress

type Installer struct {
	WorkDir      string
	Client       *http.Client
	GOOS, GOARCH string
	Worker       string
}

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

	modelDir := filepath.Join(i.WorkDir, "models", "embedding", "multilingual-e5-small", Revision)
	base := "https://huggingface.co/intfloat/multilingual-e5-small/resolve/" + Revision + "/"
	for _, a := range []struct{ name, hash string }{
		{"onnx/model.onnx", ModelHash},
		{"onnx/tokenizer.json", TokenizerHash},
	} {
		path := filepath.Join(modelDir, filepath.FromSlash(a.name))
		if err := onnxruntime.Download(ctx, i.Client, path, a.name, base+a.name, a.hash, progress); err != nil {
			return Paths{}, err
		}
	}

	runtimePath, err := onnxruntime.Ensure(ctx, onnxruntime.Config{
		WorkDir: i.WorkDir, Namespace: "embedding", Version: RuntimeVersion,
		GOOS: i.GOOS, GOARCH: i.GOARCH, Client: i.Client,
	}, progress)
	if err != nil {
		return Paths{}, err
	}
	return Paths{ModelDir: modelDir, Runtime: runtimePath, Worker: worker}, nil
}
