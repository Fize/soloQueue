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

// RunLayaWorker serves classifier requests in a subprocess of the main binary.
func RunLayaWorker(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("internal-laya", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	model := flags.String("model-dir", "", "")
	runtime := flags.String("runtime", "", "")
	device := flags.String("device", "cpu", "")
	buildID := flags.String("build-id", "", "")
	_ = flags.String("variant", "", "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *model == "" || *runtime == "" || *device != "cpu" {
		return errors.New("invalid internal laya arguments")
	}
	current, err := buildidentity.Current()
	if err != nil || *buildID == "" || *buildID != current {
		return json.NewEncoder(output).Encode(map[string]any{"v": 1, "id": 0, "error": map[string]string{"code": "build_mismatch", "message": "incompatible internal classifier build"}})
	}
	m, err := OpenLaya(*model, *runtime)
	if err != nil {
		return err
	}
	defer m.Close()
	send := func(id uint64, data, wireErr any) error {
		return json.NewEncoder(output).Encode(map[string]any{"v": 1, "id": id, "data": data, "error": wireErr})
	}
	if err = send(0, map[string]any{"variant": "laya-multilingual-fp32", "maxLen": m.maxLen, "headMaxLen": m.headMaxLen, "backend": "cpu"}, nil); err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	for {
		frame, readErr := readWorkerFrame(reader)
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if errors.Is(readErr, errFrameTooLarge) {
			if err = send(0, nil, map[string]string{"code": "too_large", "message": "frame too large"}); err != nil {
				return err
			}
			continue
		}
		if readErr != nil {
			return fmt.Errorf("read worker request: %w", readErr)
		}
		var req struct {
			Version int    `json:"v"`
			ID      uint64 `json:"id"`
			Op      string `json:"op"`
			Text    string `json:"text"`
		}
		if err = json.Unmarshal(frame, &req); err != nil || req.Version != 1 || req.ID == 0 {
			if err = send(req.ID, nil, map[string]string{"code": "bad_request", "message": "invalid request"}); err != nil {
				return err
			}
			continue
		}
		if req.Op == "shutdown" {
			return send(req.ID, map[string]bool{"stopped": true}, nil)
		}
		if req.Op != "decide" {
			if err = send(req.ID, nil, map[string]string{"code": "bad_request", "message": "invalid operation or input"}); err != nil {
				return err
			}
			continue
		}
		var state []json.RawMessage
		if err = json.Unmarshal([]byte(req.Text), &state); err != nil || len(state) == 0 {
			if err = send(req.ID, nil, map[string]string{"code": "bad_request", "message": "invalid messages"}); err != nil {
				return err
			}
			continue
		}
		result, e := m.Decide([]byte(req.Text))
		if e != nil {
			if err = send(req.ID, nil, map[string]string{"code": "model_error", "message": "local classification failed"}); err != nil {
				return err
			}
			continue
		}
		if err = send(req.ID, result, nil); err != nil {
			return err
		}
	}
}

func InternalLayaMain(args []string) int {
	if err := RunLayaWorker(args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "internal laya unavailable:", err)
		return 1
	}
	return 0
}
