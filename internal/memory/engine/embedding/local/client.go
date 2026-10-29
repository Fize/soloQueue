// Package local implements the private v1 stdin/stdout ONNX worker transport.
// It intentionally does not change the production embedding interface.
package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/xiaobaitu/soloqueue/internal/infra/buildidentity"
)

const maxFrame = 1 << 20
const Dimension = 384

// ErrInvalidInput identifies a rejected request that has not affected the worker.
var ErrInvalidInput = errors.New("invalid embedding input")

type Config struct {
	Worker, ModelDir, Runtime, Device string
	Internal                          bool
	BuildID                           string
}
type Result struct {
	Embedding []float32 `json:"embedding"`
	Tokens    int       `json:"tokens"`
	Truncated bool      `json:"truncated"`
}
type Status struct {
	Model          string  `json:"model"`
	Dimension      int     `json:"dimension"`
	Backend        string  `json:"backend"`
	FallbackReason *string `json:"fallbackReason"`
}
type Batch struct {
	Backend        string   `json:"backend"`
	FallbackReason *string  `json:"fallbackReason"`
	Results        []Result `json:"results"`
}
type wireError struct{ Code, Message string }
type response struct {
	Version int             `json:"version"`
	ID      uint64          `json:"id"`
	Data    json.RawMessage `json:"data"`
	Error   *wireError      `json:"error"`
}
type Client struct {
	cmd     *exec.Cmd
	workDir string
	stdin   io.WriteCloser
	frames  chan frame
	done    chan struct{}
	gate    chan struct{}
	once    sync.Once
	id      uint64
	status  Status
}
type frame struct {
	bytes []byte
	err   error
}

// Start starts and validates a worker, canceling and reaping it if initialization is canceled.
func Start(ctx context.Context, config Config) (*Client, error) {
	if config.Device == "" {
		config.Device = "auto"
	}
	if config.Device != "auto" && config.Device != "cpu" {
		return nil, errors.New("invalid local embedding device")
	}
	for _, path := range []string{config.Worker, config.ModelDir, config.Runtime} {
		if !filepath.IsAbs(path) {
			return nil, errors.New("local embedding paths must be absolute")
		}
	}
	args := []string{"--model-dir", config.ModelDir, "--runtime", config.Runtime, "--device", config.Device}
	if config.Internal {
		buildID, err := buildidentity.Current()
		if config.BuildID != "" {
			buildID, err = config.BuildID, nil
		}
		if err != nil {
			return nil, fmt.Errorf("identify local embedding build: %w", err)
		}
		args = append(args, "--build-id", buildID)
		args = append([]string{"__internal_embedding"}, args...)
	}
	cmd := exec.Command(config.Worker, args...)
	var workDir string
	if config.Internal {
		var e error
		workDir, e = os.MkdirTemp("", "soloqueue-embedding-*")
		if e != nil {
			return nil, e
		}
		cmd.Dir = workDir
	}
	started := false
	defer func() {
		if !started && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
	}()
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	// Discard worker stderr: native diagnostics can contain model inputs or local paths.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		return nil, err
	}
	client := &Client{cmd: cmd, stdin: input, frames: make(chan frame, 1), done: make(chan struct{}), gate: make(chan struct{}, 1), workDir: workDir}
	started = true
	client.gate <- struct{}{}
	go client.read(output)
	ready, err := client.receive(ctx, 0)
	if err == nil {
		err = strictJSON(ready.Data, &client.status)
	}
	if err == nil && (client.status.Model != "intfloat/multilingual-e5-small" || client.status.Dimension != Dimension || !validBackend(client.status.Backend)) {
		err = errors.New("invalid worker ready payload")
	}
	if err == nil && config.Device == "cpu" && client.status.Backend != "cpu" {
		err = errors.New("worker did not honor CPU mode")
	}
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
func (r *Result) UnmarshalJSON(data []byte) error {
	var wire struct {
		Embedding []float32 `json:"embedding"`
		Tokens    *int      `json:"tokens"`
		Truncated *bool     `json:"truncated"`
	}
	if err := strictJSON(data, &wire); err != nil {
		return err
	}
	if wire.Tokens == nil || wire.Truncated == nil || wire.Embedding == nil {
		return errors.New("missing embedding result fields")
	}
	*r = Result{Embedding: wire.Embedding, Tokens: *wire.Tokens, Truncated: *wire.Truncated}
	return nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func (c *Client) read(output io.Reader) {
	reader := bufio.NewScanner(output)
	reader.Buffer(make([]byte, 4096), maxFrame)
	for reader.Scan() {
		item := frame{bytes: append([]byte(nil), reader.Bytes()...)}
		select {
		case c.frames <- item:
		case <-c.done:
			return
		}
	}
	err := reader.Err()
	if err == nil {
		err = io.EOF
	}
	select {
	case c.frames <- frame{err: err}:
	case <-c.done:
	}
}
func (c *Client) receive(ctx context.Context, id uint64) (response, error) {
	select {
	case <-ctx.Done():
		_ = c.Close()
		return response{}, ctx.Err()
	case <-c.done:
		return response{}, errors.New("embedding worker closed")
	case item := <-c.frames:
		if item.err != nil {
			return response{}, fmt.Errorf("embedding transport: %w", item.err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(item.bytes, &fields) != nil || len(fields) != 4 || fields["version"] == nil || fields["id"] == nil || fields["data"] == nil || fields["error"] == nil {
			return response{}, errors.New("invalid response envelope")
		}
		var reply response
		if strictJSON(item.bytes, &reply) != nil || reply.Version != 1 || reply.ID != id {
			return response{}, errors.New("invalid embedding protocol response")
		}
		if reply.Error != nil {
			if reply.Error.Code == "build_mismatch" {
				return response{}, errors.New("incompatible internal embedding build")
			}
			return response{}, errors.New("embedding worker returned an error")
		}
		if len(reply.Data) == 0 || string(reply.Data) == "null" {
			return response{}, errors.New("missing embedding response data")
		}
		return reply, nil
	}
}
func validBackend(backend string) bool {
	return backend == "cpu" || backend == "cuda" || backend == "coreml" || backend == "directml"
}
func (c *Client) Status() Status { return c.status }

// Closed reports whether the transport has been terminated. In particular,
// cancellation while queued leaves it open, whereas active cancellation closes it.
func (c *Client) Closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Embed serializes requests. Cancellation while queued leaves the active request
// intact; cancellation during a request kills and reaps the worker.
func (c *Client) Embed(ctx context.Context, texts []string, purpose string) (Batch, error) {
	if purpose != "query" && purpose != "document" {
		return Batch{}, fmt.Errorf("%w: purpose", ErrInvalidInput)
	}
	if len(texts) == 0 || len(texts) > 32 {
		return Batch{}, fmt.Errorf("%w: batch size", ErrInvalidInput)
	}
	for _, text := range texts {
		if len(text) > 65536 || !utf8.ValidString(text) {
			return Batch{}, fmt.Errorf("%w: text size or encoding", ErrInvalidInput)
		}
	}
	select {
	case <-ctx.Done():
		return Batch{}, ctx.Err()
	case <-c.done:
		return Batch{}, errors.New("embedding worker closed")
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	c.id++
	if c.id == 0 {
		_ = c.Close()
		return Batch{}, errors.New("embedding request IDs exhausted")
	}
	payload, err := json.Marshal(struct {
		Version int      `json:"version"`
		ID      uint64   `json:"id"`
		Op      string   `json:"op"`
		Purpose string   `json:"purpose"`
		Texts   []string `json:"texts"`
	}{1, c.id, "embed", purpose, texts})
	if err != nil {
		return Batch{}, err
	}
	if len(payload)+1 > maxFrame {
		return Batch{}, fmt.Errorf("%w: frame size", ErrInvalidInput)
	}
	// Write can block if the child stops reading, so cancellation must also cover it.
	sent := make(chan error, 1)
	go func() { _, err := c.stdin.Write(append(payload, '\n')); sent <- err }()
	select {
	case <-ctx.Done():
		_ = c.Close()
		return Batch{}, ctx.Err()
	case <-c.done:
		return Batch{}, errors.New("embedding worker closed")
	case err = <-sent:
	}
	if err != nil {
		_ = c.Close()
		return Batch{}, errors.New("embedding transport write failed")
	}
	reply, err := c.receive(ctx, c.id)
	if err != nil {
		_ = c.Close()
		return Batch{}, err
	}
	var batch Batch
	if strictJSON(reply.Data, &batch) != nil || !validBackend(batch.Backend) || len(batch.Results) != len(texts) {
		_ = c.Close()
		return Batch{}, errors.New("invalid embedding batch response")
	}
	for _, result := range batch.Results {
		if len(result.Embedding) != Dimension || result.Tokens < 1 || result.Tokens > 512 {
			_ = c.Close()
			return Batch{}, errors.New("invalid embedding result shape")
		}
		norm := 0.0
		for _, v := range result.Embedding {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				_ = c.Close()
				return Batch{}, errors.New("nonfinite embedding result")
			}
			norm += float64(v) * float64(v)
		}
		if math.Abs(norm-1) > 1e-4 {
			_ = c.Close()
			return Batch{}, errors.New("invalid embedding normalization")
		}
	}
	return batch, nil
}

// Close forcibly terminates and reaps the child, including a blocked inference.
func (c *Client) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		_ = c.stdin.Close()
		_ = c.cmd.Process.Kill()
		err = c.cmd.Wait()
		if c.workDir != "" {
			err = errors.Join(err, os.RemoveAll(c.workDir))
		}
	})
	return err
}
