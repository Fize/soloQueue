package router

import (
	"context"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
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
