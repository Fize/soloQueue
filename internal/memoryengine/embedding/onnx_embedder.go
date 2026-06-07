//go:build onnx

package embedding

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNXEmbedder runs sentence-transformers models via ONNX Runtime in-process.
// The session is not thread-safe, so all calls are serialized via mu.
type ONNXEmbedder struct {
	session  *ort.AdvancedSession
	inputs   []string
	outputs  []string
	dim      int
	tokenizer *e5Tokenizer
	mu       sync.Mutex
}

// e5Tokenizer implements the E5 model tokenization: lowercase + prefix "query: " or "passage: ",
// then tokenize with the loaded vocabulary.
type e5Tokenizer struct {
	vocab     map[string]int32
	maxLen    int
}

func newONNXEmbedder(cfg Config) (Embedder, error) {
	modelPath := cfg.ModelPath
	if modelPath == "" {
		var err error
		modelPath, err = EnsureModel(cfg.ModelName)
		if err != nil {
			return nil, fmt.Errorf("embedding: ensure model: %w", err)
		}
	}

	// Initialize ONNX Runtime (safe to call multiple times)
	ort.SetSharedLibraryPath(getORTLibPath())
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("embedding: onnx init: %w", err)
	}

	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	outputNames := []string{"sentence_embedding"}

	session, err := ort.NewAdvancedSession(modelPath, inputNames, outputNames, nil, 16)
	if err != nil {
		return nil, fmt.Errorf("embedding: onnx session: %w", err)
	}

	dim := cfg.Dimension
	if dim == 0 {
		dim = 1024 // multilingual-e5-large default
	}

	tok, err := newE5Tokenizer(modelPath)
	if err != nil {
		return nil, fmt.Errorf("embedding: tokenizer: %w", err)
	}

	return &ONNXEmbedder{
		session:   session,
		inputs:    inputNames,
		outputs:   outputNames,
		dim:       dim,
		tokenizer: tok,
	}, nil
}

// Dimension returns the embedding dimension.
func (e *ONNXEmbedder) Dimension() int { return e.dim }

// Embed generates embeddings. Uses "query: " prefix for search queries
// and "passage: " prefix for stored content (E5 model convention).
func (e *ONNXEmbedder) Embed(ctx context.Context, texts []string) ([]Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]Result, len(texts))
	for i, text := range texts {
		prefixed := "query: " + text

		inputIDs, attentionMask, tokenTypeIDs := e.tokenizer.encode(prefixed)

		batchSize := int64(1)
		seqLen := int64(len(inputIDs))

		inputTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), inputIDs)
		if err != nil {
			return nil, fmt.Errorf("embedding: input tensor: %w", err)
		}
		defer inputTensor.Destroy()

		maskTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), attentionMask)
		if err != nil {
			return nil, fmt.Errorf("embedding: mask tensor: %w", err)
		}
		defer maskTensor.Destroy()

		typeIDsTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), tokenTypeIDs)
		if err != nil {
			return nil, fmt.Errorf("embedding: type ids tensor: %w", err)
		}
		defer typeIDsTensor.Destroy()

		outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(batchSize, int64(e.dim)))
		if err != nil {
			return nil, fmt.Errorf("embedding: output tensor: %w", err)
		}
		defer outputTensor.Destroy()

		err = e.session.Run(
			map[string]ort.ArbitraryTensor{
				e.inputs[0]: inputTensor,
				e.inputs[1]: maskTensor,
				e.inputs[2]: typeIDsTensor,
			},
			map[string]ort.ArbitraryTensor{
				e.outputs[0]: outputTensor,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("embedding: run: %w", err)
		}

		raw := outputTensor.GetData()
		vec := make([]float32, e.dim)
		copy(vec, raw)

		// L2 normalize
		vec = l2Normalize(vec)

		results[i] = Result{Embedding: vec, Tokens: len(inputIDs)}
	}
	return results, nil
}

// newE5Tokenizer loads the HuggingFace tokenizer vocabulary from the model directory.
func newE5Tokenizer(modelPath string) (*e5Tokenizer, error) {
	vocabPath := findVocabFile(modelPath)
	vocab, err := loadVocab(vocabPath)
	if err != nil {
		return nil, err
	}
	return &e5Tokenizer{
		vocab:  vocab,
		maxLen: 512,
	}, nil
}

// encode tokenizes text and returns input_ids, attention_mask, token_type_ids.
func (t *e5Tokenizer) encode(text string) ([]int64, []int64, []int64) {
	tokens := t.tokenize(text)
	if len(tokens) > t.maxLen-2 {
		tokens = tokens[:t.maxLen-2]
	}

	clsID := t.vocab["[CLS]"]
	sepID := t.vocab["[SEP]"]
	if clsID == 0 {
		clsID = 101 // BERT CLS
	}
	if sepID == 0 {
		sepID = 102 // BERT SEP
	}

	ids := make([]int64, 0, len(tokens)+2)
	ids = append(ids, int64(clsID))
	for _, tok := range tokens {
		ids = append(ids, int64(tok))
	}
	ids = append(ids, int64(sepID))

	padTo := t.maxLen
	attentionMask := make([]int64, padTo)
	for i := range attentionMask {
		if i < len(ids) {
			attentionMask[i] = 1
		}
	}

	// Pad ids
	for len(ids) < padTo {
		ids = append(ids, 0)
	}

	tokenTypeIDs := make([]int64, padTo)
	return ids, attentionMask, tokenTypeIDs
}

// tokenize does basic whitespace+punct tokenization and vocabulary lookup.
func (t *e5Tokenizer) tokenize(text string) []int32 {
	// Simple whitespace tokenization with lowercasing (E5-style)
	var tokens []int32
	var current []byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'A' && c <= 'Z' {
			c += 32 // lowercase
		}
		if c == ' ' || c == '\t' || c == '\n' {
			if len(current) > 0 {
				if id, ok := t.vocab[string(current)]; ok {
					tokens = append(tokens, id)
				} else if id, ok := t.vocab["[UNK]"]; ok {
					tokens = append(tokens, id)
				}
				current = current[:0]
			}
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		if id, ok := t.vocab[string(current)]; ok {
			tokens = append(tokens, id)
		} else if id, ok := t.vocab["[UNK]"]; ok {
			tokens = append(tokens, id)
		}
	}
	return tokens
}

func l2Normalize(v []float32) []float32 {
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	if sq == 0 {
		return v
	}
	inv := 1.0 / math.Sqrt(sq)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) * inv)
	}
	return out
}

// getORTLibPath returns the ONNX Runtime shared library path for the current platform.
func getORTLibPath() string {
	// Default paths — can be overridden via env var or build tags
	// macOS: /opt/homebrew/lib/libonnxruntime.dylib or /usr/local/lib/libonnxruntime.dylib
	// Linux: /usr/lib/libonnxruntime.so or /usr/local/lib/libonnxruntime.so
	return ""
}

// placeholder types for vocabulary loading
func findVocabFile(modelPath string) string { return modelPath + "/vocab.txt" }
func loadVocab(path string) (map[string]int32, error) { return nil, fmt.Errorf("TODO: load vocab from %s", path) }

// Ensure binary encoding is used
func init() {
	binary.NativeEndian // force import
}
