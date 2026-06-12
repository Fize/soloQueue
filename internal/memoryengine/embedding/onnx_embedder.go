package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNXEmbedder runs sentence-transformers models via ONNX Runtime in-process.
// The session is not thread-safe, so all calls are serialized via mu.
type ONNXEmbedder struct {
	session  *ort.DynamicAdvancedSession
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

	onnxFile := filepath.Join(modelPath, "model.onnx")

	// Initialize ONNX Runtime (safe to call multiple times)
	ort.SetSharedLibraryPath(getORTLibPath())
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("embedding: onnx init: %w", err)
	}

	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	outputNames := []string{"sentence_embedding"}

	options, err := ort.NewSessionOptions()
	if err == nil {
		options.AppendExecutionProviderCoreML(0)
	}
	session, err := ort.NewDynamicAdvancedSession(onnxFile, inputNames, outputNames, options)
	if err != nil && options != nil {
		session, err = ort.NewDynamicAdvancedSession(onnxFile, inputNames, outputNames, nil)
	}
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
			[]ort.Value{inputTensor, maskTensor, typeIDsTensor},
			[]ort.Value{outputTensor},
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

// tokenize performs SentencePiece-style longest-match tokenization.
// Spaces are replaced with "▁" (U+2581) and the text is split into known subword tokens.
func (t *e5Tokenizer) tokenize(text string) []int32 {
	processed := make([]byte, 0, len(text)+32)
	processed = append(processed, 0xe2, 0x96, 0x81)
	inSpace := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !inSpace {
				processed = append(processed, 0xe2, 0x96, 0x81)
				inSpace = true
			}
		} else {
			processed = append(processed, c)
			inSpace = false
		}
	}

	var tokens []int32
	runes := []rune(string(processed))
	unkID := t.vocab["<unk>"]
	if unkID == 0 {
		unkID = 1
	}

	for i := 0; i < len(runes); {
		longestLen := 0
		longestID := int32(0)

		for j := i + 1; j <= len(runes) && j-i <= 32; j++ {
			candidate := string(runes[i:j])
			if id, ok := t.vocab[candidate]; ok {
				longestLen = j - i
				longestID = id
			}
		}

		if longestLen > 0 {
			tokens = append(tokens, longestID)
			i += longestLen
		} else {
			tokens = append(tokens, unkID)
			i++
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
	paths := []string{
		"/opt/homebrew/lib/libonnxruntime.dylib",
		"/usr/local/lib/libonnxruntime.dylib",
		"/usr/lib/libonnxruntime.so",
		"/usr/local/lib/libonnxruntime.so",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// placeholder types for vocabulary loading
func findVocabFile(modelPath string) string {
	p := filepath.Join(modelPath, "tokenizer.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(modelPath, "vocab.txt")
}

func loadVocab(path string) (map[string]int32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vocab: %w", err)
	}

	if filepath.Base(path) == "tokenizer.json" {
		return parseTokenizerJSON(data)
	}
	return parseVocabTxt(data)
}

func parseTokenizerJSON(data []byte) (map[string]int32, error) {
	var tok struct {
		Model struct {
			Vocab [][]interface{} `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}

	vocab := make(map[string]int32, len(tok.Model.Vocab))
	for i, entry := range tok.Model.Vocab {
		if len(entry) > 0 {
			if tokStr, ok := entry[0].(string); ok {
				vocab[tokStr] = int32(i)
			}
		}
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("tokenizer.json has empty vocab")
	}
	return vocab, nil
}

func parseVocabTxt(data []byte) (map[string]int32, error) {
	lines := splitLines(string(data))
	vocab := make(map[string]int32, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		token := line
		if idx := indexOfSpace(line); idx >= 0 {
			token = line[:idx]
		}
		vocab[token] = int32(i)
	}
	return vocab, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		} else if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				lines = append(lines, s[start:i])
				i++
				start = i + 1
			} else {
				lines = append(lines, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func indexOfSpace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return i
		}
	}
	return -1
}

