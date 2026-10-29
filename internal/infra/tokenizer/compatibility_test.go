package sentencepiece

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Expected token IDs come from the matching tokenizers versions used by the existing models.
func TestModelTokenizationCompatibility(t *testing.T) {
	tests := []struct {
		name, env string
		inputs    []string
		want      [][]int
	}{
		{"laya", "SOLOQUEUE_TEST_LAYA_TOKENIZER", []string{"Hello", "  <mask>", "query: hello  world", "😀 café e\u0301"}, [][]int{{25957}, {4}, {8164, 235292, 25612, 235248, 2134}, {79937, 18688, 526, 236703}}},
		{"embedding", "SOLOQUEUE_TEST_EMBEDDING_TOKENIZER", []string{"Hello", "  <mask>", "query: hello  world", "😀 café e\u0301"}, [][]int{{35378}, {6, 250001}, {41, 1294, 12, 33600, 31, 8999}, {21119, 26216, 393}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := os.Getenv(tc.env)
			if path == "" {
				t.Skip("model tokenizer path not configured")
			}
			tok, err := NewTokenizer(path)
			if err != nil {
				t.Fatal(err)
			}
			for i, input := range tc.inputs {
				ids, err := tok.Encode(input)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ids, tc.want[i]) {
					t.Errorf("case %d: got %v, want %v", i, ids, tc.want[i])
				}
			}
		})
	}
}

func TestEmbeddingTokenizerReferenceFixture(t *testing.T) {
	path, fixture := os.Getenv("SOLOQUEUE_TEST_EMBEDDING_TOKENIZER"), os.Getenv("SOLOQUEUE_ONNX_TOKEN_FIXTURE")
	if path == "" || fixture == "" {
		t.Skip("tokenizer and reference fixture not configured")
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text      string `json:"text"`
		Purpose   string `json:"purpose"`
		IDs       []int  `json:"input_ids"`
		Mask      []int  `json:"attention_mask"`
		Truncated bool   `json:"truncated"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty reference fixture")
	}
	tok, err := NewTokenizer(path)
	if err != nil {
		t.Fatal(err)
	}
	m := tok.Model()
	for i, tc := range cases {
		prefix := "passage: "
		if tc.Purpose == "query" {
			prefix = "query: "
		}
		ids, err := tok.Encode(prefix + tc.Text)
		if err != nil {
			t.Fatal(err)
		}
		truncated := len(ids)+2 > 512
		if len(ids) > 510 {
			ids = ids[:510]
		}
		actual := append([]int{m.PieceToId("<s>")}, ids...)
		actual = append(actual, m.PieceToId("</s>"))
		mask := make([]int, 512)
		for j := range actual {
			mask[j] = 1
		}
		for len(actual) < 512 {
			actual = append(actual, m.PieceToId("<pad>"))
		}
		if truncated != tc.Truncated || !reflect.DeepEqual(actual, tc.IDs) || !reflect.DeepEqual(mask, tc.Mask) {
			t.Fatalf("reference tokenization mismatch for case %d", i)
		}
	}
}
