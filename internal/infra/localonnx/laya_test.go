//go:build cgo

package localonnx

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestLayaReferenceResponses compares Go results with the pinned pre-migration CPU model.
func TestLayaReferenceResponses(t *testing.T) {
	model, runtime := os.Getenv("SOLOQUEUE_TEST_LAYA_MODEL"), os.Getenv("SOLOQUEUE_TEST_LAYA_RUNTIME")
	if model == "" || runtime == "" {
		t.Skip("real model and runtime not configured")
	}
	data, err := os.ReadFile("testdata/laya_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		State  json.RawMessage `json:"state"`
		Result LayaResult      `json:"result"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty reference fixture")
	}
	m, err := OpenLaya(model, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for i, tc := range cases {
		got, e := m.Decide(tc.State)
		if e != nil {
			t.Fatal(e)
		}
		if got.Choice != tc.Result.Choice {
			t.Fatalf("case %d choice mismatch", i)
		}
		for _, label := range layaLabels {
			if math.Abs(got.Probabilities[label]-tc.Result.Probabilities[label]) > 1e-3 {
				t.Fatalf("case %d probability mismatch for %s", i, label)
			}
		}
	}
}

func TestPythonJSONPreservesUnicodeAndEscaping(t *testing.T) {
	raw := []byte(`[{"role":"user","content":"<x>\u2028\n\"\\u2028"}]`)
	got, err := pythonJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "[{\"role\": \"user\", \"content\": \"<x>\u2028\\n\\\"\\\\u2028\"}]"
	if got != want {
		t.Fatalf("unexpected serialized state: %q", got)
	}
}
