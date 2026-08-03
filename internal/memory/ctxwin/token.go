// Package ctxwin provides an in-memory, rule-based linear context truncator.
//
// The package name uses ctxwin (not context) to avoid conflicts with the Go standard library's context package.
// This project heavily imports "context" for context.Context, and a name collision would necessitate aliasing.
package ctxwin

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer wraps tiktoken-go to provide token counting capability.
//
// Thread-safe: The internal `enc` is read-only, so Count can be called concurrently.
// It uses cl100k_base encoding, which approximates DeepSeek tokenization.
// This is primarily used to estimate the token count of new push messages; most counts are precisely calibrated by the API.
type Tokenizer struct {
	enc *tiktoken.Tiktoken
}

var (
	tokenizerOnce sync.Once
	defaultEnc    *tiktoken.Tiktoken
)

// NewTokenizer creates a Tokenizer (using cl100k_base encoding).
//
// It uses sync.Once internally to ensure the encoding is loaded only once (BPE rank file I/O).
// Multiple calls will return Tokenizers sharing the same underlying encoding instance.
func NewTokenizer() *Tokenizer {
	tokenizerOnce.Do(func() {
		defaultEnc, _ = tiktoken.EncodingForModel("gpt-4")
	})
	return &Tokenizer{enc: defaultEnc}
}

// Count returns an estimation of the token count for the given text.
//
// Returns 0 for an empty string.
// Note: cl100k_base is an approximation for DeepSeek, with errors typically within 5-15%.
// Through the Calibrate mechanism, each API call recalibrates with the exact value, so estimation errors do not accumulate.
func (t *Tokenizer) Count(text string) int {
	if text == "" || t.enc == nil {
		return 0
	}
	return len(t.enc.Encode(text, nil, nil))
}

// EstimateByLen quickly estimates the token count based on character length, without calling BPE encoding.
//
// This is used for scenarios where high precision is not required, such as re-estimating token counts after truncation.
// Ratio: Approximately 3.3 bytes per token (an empirical value for cl100k_base with mixed Chinese and English text).
// The error is acceptable because Calibrate will recalibrate on the next API call.
func (t *Tokenizer) EstimateByLen(text string) int {
	if text == "" {
		return 0
	}
	// 3.3 bytes/token, round up to avoid underestimation
	return (len(text) + 2) / 3
}