package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/llm/supervised"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func TestLLMClassifierAllowsModerateProviderLatency(t *testing.T) {
	classifier := NewLLMClassifier(
		&agenttest.FakeLLM{
			Delay:     3 * time.Second,
			Responses: []string{`{"task_type":"engineering"}`},
		},
		"provider",
		"model",
	)

	got, err := classifier.Classify(context.Background(), ClassifyInput{Text: "inspect the router"}, nil)
	if err != nil {
		t.Fatalf("Classify() error = %v, want a response within the classifier deadline", err)
	}
	if got != tasktype.Engineering {
		t.Fatalf("Classify() = %q, want %q", got, tasktype.Engineering)
	}
}

func TestLLMClassifierFailureDoesNotCancelSessionRoot(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour})
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), runwatch.Metadata{RunID: "classifier-root"})
	if err != nil {
		t.Fatal(err)
	}
	client := supervised.New(&agenttest.FakeLLM{Err: errors.New("classifier unavailable")}, manager)
	classifier := NewLLMClassifier(client, "provider", "model")

	if _, err := classifier.Classify(ctx, ClassifyInput{Text: "continue"}, nil); err == nil {
		t.Fatal("Classify() error = nil, want provider failure")
	}
	if _, ok := root.Snapshot(); !ok {
		t.Fatal("classifier failure terminated the session root")
	}
	root.Complete()
}

func TestLLMClassifierPreservesMalformedResponseForDiagnostics(t *testing.T) {
	classifier := NewLLMClassifier(
		&agenttest.FakeLLM{Responses: []string{"based on the request, this is engineering"}},
		"provider",
		"model",
	)

	_, err := classifier.Classify(context.Background(), ClassifyInput{Text: "inspect the router"}, nil)
	if err == nil {
		t.Fatal("Classify() error = nil, want malformed response error")
	}
	responseErr, ok := err.(*classifierResponseError)
	if !ok {
		t.Fatalf("error type = %T, want *classifierResponseError", err)
	}
	if responseErr.Content != "based on the request, this is engineering" {
		t.Fatalf("response content = %q", responseErr.Content)
	}
}
