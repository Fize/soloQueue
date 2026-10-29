//go:build cgo

package localonnx

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xiaobaitu/soloqueue/internal/infra/buildidentity"
)

// RunEmbeddingWorker serves the private embedding transport over standard IO.
func RunEmbeddingWorker(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("internal-embedding", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	model := flags.String("model-dir", "", "")
	runtime := flags.String("runtime", "", "")
	device := flags.String("device", "cpu", "")
	buildID := flags.String("build-id", "", "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *model == "" || *runtime == "" || (*device != "cpu" && *device != "auto") {
		return errors.New("invalid internal embedding arguments")
	}
	current, err := buildidentity.Current()
	if err != nil || *buildID == "" || *buildID != current {
		return json.NewEncoder(output).Encode(map[string]any{"version": 1, "id": 0, "data": nil, "error": map[string]string{"code": "build_mismatch", "message": "incompatible internal embedding build"}})
	}
	m, err := OpenEmbeddingWithDevice(*model, *runtime, *device)
	if err != nil {
		return err
	}
	defer m.Close()
	send := func(id uint64, data any, wireErr any) error {
		return json.NewEncoder(output).Encode(map[string]any{"version": 1, "id": id, "data": data, "error": wireErr})
	}
	backend, fallback := m.Backend()
	if err = send(0, map[string]any{"model": "intfloat/multilingual-e5-small", "dimension": embeddingDimension, "backend": backend, "fallbackReason": fallback}, nil); err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	for {
		frame, readErr := readWorkerFrame(reader)
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if errors.Is(readErr, errFrameTooLarge) {
			if err = send(0, nil, map[string]string{"code": "frame_too_large", "message": "frame too large"}); err != nil {
				return err
			}
			continue
		}
		if readErr != nil {
			return fmt.Errorf("read worker request: %w", readErr)
		}
		var req struct {
			Version int      `json:"version"`
			ID      uint64   `json:"id"`
			Op      string   `json:"op"`
			Purpose string   `json:"purpose"`
			Texts   []string `json:"texts"`
		}
		if err = json.Unmarshal(frame, &req); err != nil || req.Version != 1 || req.ID == 0 {
			if err = send(0, nil, map[string]string{"code": "invalid_request", "message": "invalid request"}); err != nil {
				return err
			}
			continue
		}
		if req.Op == "shutdown" {
			return send(req.ID, map[string]any{}, nil)
		}
		if req.Op != "embed" || (req.Purpose != "query" && req.Purpose != "document") || len(req.Texts) == 0 || len(req.Texts) > 32 {
			if err = send(req.ID, nil, map[string]string{"code": "invalid_request", "message": "invalid operation or batch"}); err != nil {
				return err
			}
			continue
		}

		valid := true
		for _, text := range req.Texts {
			if len(text) > 65536 {
				valid = false
				break
			}
		}
		var results []EmbeddingResult
		if valid {
			var e error
			results, e = m.EmbedBatch(req.Texts, req.Purpose)
			valid = e == nil
		}
		if !valid {
			if err = send(req.ID, nil, map[string]string{"code": "inference_failed", "message": "local inference failed"}); err != nil {
				return err
			}
			continue
		}
		backend, fallback = m.Backend()
		if err = send(req.ID, map[string]any{"backend": backend, "fallbackReason": fallback, "results": results}, nil); err != nil {
			return err
		}
	}
}

// InternalEmbeddingMain serves the isolated process and reports initialization errors.
func InternalEmbeddingMain(args []string) int {
	if err := RunEmbeddingWorker(args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "internal embedding unavailable:", err)
		return 1
	}
	return 0
}
