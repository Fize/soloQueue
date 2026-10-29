//go:build cgo

package localonnx

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	ort "github.com/yalue/onnxruntime_go"
)

// TestEmbeddingReferenceVectors uses vectors captured from the pinned pre-migration CPU worker.
func TestEmbeddingReferenceVectors(t *testing.T) {
	model, runtime := os.Getenv("SOLOQUEUE_TEST_EMBEDDING_MODEL"), os.Getenv("SOLOQUEUE_TEST_EMBEDDING_RUNTIME")
	if model == "" || runtime == "" {
		t.Skip("real model and runtime not configured")
	}
	raw, err := os.ReadFile("testdata/embedding_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text    string          `json:"text"`
		Purpose string          `json:"purpose"`
		Result  EmbeddingResult `json:"result"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	m, err := OpenEmbedding(model, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for i, tc := range cases {
		got, e := m.Embed(tc.Text, tc.Purpose)
		if e != nil {
			t.Fatal(e)
		}
		want := tc.Result
		if got.Tokens != want.Tokens || got.Truncated != want.Truncated || len(got.Embedding) != len(want.Embedding) {
			t.Fatalf("case %d metadata mismatch", i)
		}
		for j, v := range got.Embedding {
			if math.Abs(float64(v-want.Embedding[j])) > 1e-5 {
				t.Fatalf("case %d vector differs at %d", i, j)
			}
		}
	}
}

type failingEmbeddingSession struct {
	embeddingSessionRunner
	calls int
}

func (s *failingEmbeddingSession) Run(inputs, outputs []ort.Value) error {
	s.calls++
	if s.calls == 2 {
		return errors.New("injected inference failure")
	}
	return s.embeddingSessionRunner.Run(inputs, outputs)
}

func TestEmbeddingRetriesWholeBatchOnCPU(t *testing.T) {
	model, runtime := os.Getenv("SOLOQUEUE_TEST_EMBEDDING_MODEL"), os.Getenv("SOLOQUEUE_TEST_EMBEDDING_RUNTIME")
	if model == "" || runtime == "" {
		t.Skip("real model and runtime not configured")
	}
	m, err := OpenEmbedding(model, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	texts := []string{"first item", "second item"}
	expected, err := m.EmbedBatch(texts, "query")
	if err != nil {
		t.Fatal(err)
	}
	failing := &failingEmbeddingSession{embeddingSessionRunner: m.session}
	m.session = failing
	m.backend = "coreml"
	actual, err := m.EmbedBatch(texts, "query")
	if err != nil {
		t.Fatal(err)
	}
	backend, reason := m.Backend()
	if backend != "cpu" || reason == nil || failing.calls != 2 {
		t.Fatalf("unexpected recovery state %s, %v, %d", backend, reason, failing.calls)
	}
	if len(actual) != len(expected) {
		t.Fatal("batch length changed")
	}
	for i := range actual {
		for j, v := range actual[i].Embedding {
			if math.Abs(float64(v-expected[i].Embedding[j])) > 1e-5 {
				t.Fatalf("result %d differs at %d", i, j)
			}
		}
	}
}
