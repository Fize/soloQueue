//go:build cgo

// Package localonnx contains the local model inference implementations.
package localonnx

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/infra/tokenizer"
	ort "github.com/yalue/onnxruntime_go"
)

const embeddingDimension = 384
const embeddingMaxTokens = 512

// Embedding is an isolated ONNX model instance; its process owns the ORT environment.
type embeddingSessionRunner interface {
	Run([]ort.Value, []ort.Value) error
	Destroy() error
}

type Embedding struct {
	mu            sync.Mutex
	tokenizer     *sentencepiece.Tokenizer
	session       embeddingSessionRunner
	cls, sep, pad int
	modelPath     string
	backend       string
	fallback      *string
}

type EmbeddingResult struct {
	Embedding []float32 `json:"embedding"`
	Tokens    int       `json:"tokens"`
	Truncated bool      `json:"truncated"`
}

// OpenEmbedding opens the model in CPU mode.
func OpenEmbedding(modelDir, runtimePath string) (*Embedding, error) {
	return OpenEmbeddingWithDevice(modelDir, runtimePath, "cpu")
}

// OpenEmbeddingWithDevice uses a verified accelerator when auto mode permits it.
func OpenEmbeddingWithDevice(modelDir, runtimePath, device string) (*Embedding, error) {
	if device != "cpu" && device != "auto" {
		return nil, errors.New("invalid embedding device")
	}
	tok, err := sentencepiece.NewTokenizer(filepath.Join(modelDir, "onnx", "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	model := tok.Model()
	cls, sep, pad := model.PieceToId("<s>"), model.PieceToId("</s>"), model.PieceToId("<pad>")
	if cls < 0 || sep < 0 || pad < 0 {
		return nil, errors.New("embedding tokenizer missing special tokens")
	}
	if err = configureRuntimeDirectory(runtimePath); err != nil {
		return nil, err
	}
	ort.SetSharedLibraryPath(runtimePath)
	if err = ort.InitializeEnvironment(); err != nil {
		return nil, err
	}
	m := &Embedding{tokenizer: tok, cls: cls, sep: sep, pad: pad, modelPath: filepath.Join(modelDir, "onnx", "model.onnx"), backend: "cpu"}
	if device == "auto" {
		candidates := []string{"cuda"}
		if runtime.GOOS == "darwin" {
			candidates = []string{"coreml"}
		}
		if runtime.GOOS == "windows" {
			candidates = []string{"cuda", "directml"}
		}
		var failures []string
		for _, candidate := range candidates {
			if e := m.tryAccelerator(candidate); e == nil {
				return m, nil
			} else {
				failures = append(failures, candidate+": "+e.Error())
			}
		}
		reason := strings.Join(failures, "; ")
		m.fallback = &reason
	}
	m.session, err = embeddingSession(m.modelPath, "cpu", "")
	if err != nil {
		_ = ort.DestroyEnvironment()
		return nil, err
	}
	if _, err = m.embedLocked("warmup", "query"); err != nil {
		_ = m.session.Destroy()
		_ = ort.DestroyEnvironment()
		return nil, err
	}
	return m, nil
}

func embeddingSession(modelPath, backend, profile string) (*ort.DynamicAdvancedSession, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	if err = opts.SetIntraOpNumThreads(2); err != nil {
		return nil, err
	}
	switch backend {
	case "cpu":
	case "coreml":
		err = opts.AppendExecutionProviderCoreMLV2(map[string]string{"MLComputeUnits": "CPUAndGPU"})
	case "cuda":
		var c *ort.CUDAProviderOptions
		c, err = ort.NewCUDAProviderOptions()
		if err == nil {
			err = opts.AppendExecutionProviderCUDA(c)
			_ = c.Destroy()
		}
	case "directml":
		if err = opts.SetExecutionMode(ort.ExecutionModeSequential); err == nil {
			err = opts.SetMemPattern(false)
		}
		if err == nil {
			err = opts.AppendExecutionProviderDirectML(0)
		}
	default:
		return nil, errors.New("unknown embedding backend")
	}
	if err != nil {
		return nil, err
	}
	if profile != "" {
		if err = opts.EnableProfiling(profile); err != nil {
			return nil, err
		}
	}
	return ort.NewDynamicAdvancedSession(modelPath, []string{"input_ids", "attention_mask", "token_type_ids"}, []string{"last_hidden_state"}, opts)
}

// tryAccelerator requires profiling evidence that at least one node used the provider.
func (m *Embedding) tryAccelerator(backend string) error {
	dir, err := os.MkdirTemp("", "soloqueue-embedding-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	m.session, err = embeddingSession(m.modelPath, backend, filepath.Join(dir, "profile"))
	if err != nil {
		return err
	}
	_, warmErr := m.embedLocked("warmup", "query")
	closeErr := m.session.Destroy()
	m.session = nil
	if warmErr != nil {
		return fmt.Errorf("warmup failed: %w", warmErr)
	}
	if closeErr != nil {
		return closeErr
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := map[string]string{"coreml": "CoreMLExecutionProvider", "cuda": "CUDAExecutionProvider", "directml": "DmlExecutionProvider"}
	verified := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			continue
		}
		var events []struct {
			Args struct {
				Provider string `json:"provider"`
			} `json:"args"`
		}
		if json.Unmarshal(data, &events) != nil {
			continue
		}
		for _, event := range events {
			if event.Args.Provider == names[backend] {
				verified = true
				break
			}
		}
	}
	if !verified {
		return errors.New("no profiled accelerator nodes")
	}
	m.session, err = embeddingSession(m.modelPath, backend, "")
	if err != nil {
		return err
	}
	m.backend = backend
	return nil
}

// Backend reports the verified execution provider and any CPU fallback reason.
func (m *Embedding) Backend() (string, *string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backend, m.fallback
}

// Embed applies the pinned model's query/document prefixes, pooling and normalization.
func (m *Embedding) Embed(text, purpose string) (EmbeddingResult, error) {
	results, err := m.EmbedBatch([]string{text}, purpose)
	if err != nil {
		return EmbeddingResult{}, err
	}
	return results[0], nil
}

// EmbedBatch retries the entire batch on CPU if a verified accelerator fails.
func (m *Embedding) EmbedBatch(texts []string, purpose string) ([]EmbeddingResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := func() ([]EmbeddingResult, error) {
		results := make([]EmbeddingResult, 0, len(texts))
		for _, text := range texts {
			value, err := m.embedLocked(text, purpose)
			if err != nil {
				return nil, err
			}
			results = append(results, value)
		}
		return results, nil
	}
	result, err := run()
	if err == nil || m.backend == "cpu" {
		return result, err
	}
	cpu, e := embeddingSession(m.modelPath, "cpu", "")
	if e != nil {
		return nil, errors.Join(err, e)
	}
	old := m.session
	m.session = cpu
	previous := m.backend
	m.backend = "cpu"
	reason := previous + ": inference failed; entire request retried on CPU"
	m.fallback = &reason
	_ = old.Destroy()
	return run()
}

func (m *Embedding) embedLocked(text, purpose string) (EmbeddingResult, error) {
	if m.session == nil {
		return EmbeddingResult{}, errors.New("embedding model closed")
	}
	var prefix string
	switch purpose {
	case "query":
		prefix = "query: "
	case "document":
		prefix = "passage: "
	default:
		return EmbeddingResult{}, errors.New("invalid embedding purpose")
	}
	ids, err := m.tokenizer.Encode(prefix + text)
	if err != nil {
		return EmbeddingResult{}, err
	}
	truncated := len(ids)+2 > embeddingMaxTokens
	if len(ids) > embeddingMaxTokens-2 {
		ids = ids[:embeddingMaxTokens-2]
	}
	count := len(ids) + 2
	input := make([]int64, embeddingMaxTokens)
	mask := make([]int64, embeddingMaxTokens)
	types := make([]int64, embeddingMaxTokens)
	for i := range input {
		input[i] = int64(m.pad)
	}
	input[0] = int64(m.cls)
	for i, id := range ids {
		input[i+1] = int64(id)
	}
	input[count-1] = int64(m.sep)
	for i := 0; i < count; i++ {
		mask[i] = 1
	}
	shape := ort.NewShape(1, embeddingMaxTokens)
	a, err := ort.NewTensor(shape, input)
	if err != nil {
		return EmbeddingResult{}, err
	}
	defer a.Destroy()
	b, err := ort.NewTensor(shape, mask)
	if err != nil {
		return EmbeddingResult{}, err
	}
	defer b.Destroy()
	c, err := ort.NewTensor(shape, types)
	if err != nil {
		return EmbeddingResult{}, err
	}
	defer c.Destroy()
	outputs := []ort.Value{nil}
	if err = m.session.Run([]ort.Value{a, b, c}, outputs); err != nil {
		return EmbeddingResult{}, err
	}
	defer outputs[0].Destroy()
	tensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return EmbeddingResult{}, errors.New("unexpected embedding output type")
	}
	data := tensor.GetData()
	if len(data) != embeddingMaxTokens*embeddingDimension {
		return EmbeddingResult{}, fmt.Errorf("unexpected embedding output length %d", len(data))
	}
	v := make([]float32, embeddingDimension)
	for i := 0; i < count; i++ {
		for j := range v {
			v[j] += data[i*embeddingDimension+j] / float32(count)
		}
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return EmbeddingResult{}, errors.New("invalid embedding norm")
	}
	for i := range v {
		v[i] /= float32(norm)
	}
	return EmbeddingResult{Embedding: v, Tokens: count, Truncated: truncated}, nil
}

func (m *Embedding) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil
	}
	err := m.session.Destroy()
	m.session = nil
	return errors.Join(err, ort.DestroyEnvironment())
}
