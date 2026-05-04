package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAI_Embed_Single(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected /embeddings, got %s", r.URL.Path)
		}
		resp := embedResponse{
			Data: []embedData{
				{Index: 0, Embedding: []float32{1, 2, 3}},
			},
			Usage: &embedUsage{PromptTokens: 2, TotalTokens: 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, err := NewOpenAI(OpenAIConfig{
		BaseURL:    srv.URL,
		ModelID:    "test-model",
		Dimension:  3,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}

	results, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Embedding) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(results[0].Embedding))
	}
	if client.Dimension() != 3 {
		t.Errorf("Dimension: expected 3, got %d", client.Dimension())
	}
}

func TestOpenAI_Embed_Batch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Data: []embedData{
				{Index: 0, Embedding: []float32{1, 0}},
				{Index: 1, Embedding: []float32{0, 1}},
			},
			Usage: &embedUsage{PromptTokens: 4, TotalTokens: 4},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, _ := NewOpenAI(OpenAIConfig{
		BaseURL:    srv.URL,
		ModelID:    "test-model",
		Dimension:  2,
		HTTPClient: srv.Client(),
	})

	results, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestOpenAI_Embed_EmptyInput(t *testing.T) {
	client, _ := NewOpenAI(OpenAIConfig{
		BaseURL:   "http://localhost",
		ModelID:   "test",
		Dimension: 3,
	})
	results, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
}

func TestOpenAI_NewOpenAI_MissingConfig(t *testing.T) {
	_, err := NewOpenAI(OpenAIConfig{BaseURL: "", ModelID: ""})
	if err == nil {
		t.Error("expected error for empty config")
	}
	_, err = NewOpenAI(OpenAIConfig{BaseURL: "http://localhost", ModelID: ""})
	if err == nil {
		t.Error("expected error for missing ModelID")
	}
}
