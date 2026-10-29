package router

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type inputDecider struct{ input string }

func (d *inputDecider) Decide(_ context.Context, text string) (tasktype.TaskType, error) {
	d.input = text
	return tasktype.Engineering, nil
}

func TestLayaReceivesSharedClassifierMessages(t *testing.T) {
	d := &inputDecider{}
	c := NewDefaultClassifier(ClassifierConfig{Engine: EngineLaya}, nil, "", "", d, nil)
	input := ClassifyInput{Text: routerSample("sample_005"), PreviousTaskType: tasktype.Engineering, HasImages: true}
	history := []ctxwin.PayloadMessage{{Role: "user", Content: routerSample("sample_001")}, {Role: "assistant", Content: "Done"}}
	c.Classify(context.Background(), input, history)
	var got []agent.LLMMessage
	if err := json.Unmarshal([]byte(d.input), &got); err != nil {
		t.Fatal(err)
	}
	want := classifierMessages(input, history)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestLocalClassifierOnlyShortCircuitsStrongEvidence(t *testing.T) {
	c := NewLocalClassifier()
	for _, tt := range []struct {
		input   string
		want    tasktype.TaskType
		matched bool
	}{
		{"run go test ./...", tasktype.Engineering, true},
		{routerSample("sample_011"), tasktype.Engineering, true},
		{routerSample("sample_012"), tasktype.Engineering, true},
		{routerSample("sample_013"), tasktype.Engineering, true},
		{routerSample("sample_014"), tasktype.Engineering, true},
		{routerSample("sample_015"), tasktype.Engineering, true},
		{"inspect the router failure", tasktype.Engineering, true},
		{routerSample("sample_016"), tasktype.Engineering, true},
		{routerSample("sample_017"), tasktype.Engineering, true},
		{routerSample("sample_018"), tasktype.Engineering, true},
		{routerSample("sample_019"), tasktype.Engineering, true},
		{routerSample("sample_020"), tasktype.Engineering, true},
		{routerSample("sample_021"), tasktype.Engineering, true},
		{routerSample("sample_022"), tasktype.Engineering, true},
		{routerSample("sample_023"), tasktype.Engineering, true},
		{routerSample("sample_024"), tasktype.Engineering, true},
		{"trace the wire failure", tasktype.Engineering, true},
		{"update the prompt", tasktype.Engineering, true},
		{routerSample("sample_025"), tasktype.Engineering, true},
		{routerSample("sample_026"), tasktype.Research, true},
		{routerSample("sample_027"), tasktype.General, true},
		{routerSample("sample_003"), tasktype.Unknown, false},
		{routerSample("sample_028"), tasktype.Unknown, false},
		{routerSample("sample_029"), tasktype.Unknown, false},
		{routerSample("sample_030"), tasktype.Unknown, false},
		{routerSample("sample_031"), tasktype.Unknown, false},
		{routerSample("sample_032"), tasktype.Unknown, false},
		{routerSample("sample_033"), tasktype.Unknown, false},
		{routerSample("sample_034"), tasktype.Unknown, false},
		{"update capitalization", tasktype.Unknown, false},
	} {
		got := c.Classify(tt.input)
		if got.Matched != tt.matched || got.TaskType != tt.want {
			t.Errorf("Classify(%q) = %+v, want matched=%v type=%s", tt.input, got, tt.matched, tt.want)
		}
	}
}

func TestDefaultClassifierFallsBackAfterLLMFailure(t *testing.T) {
	classifier := NewDefaultClassifier(
		ClassifierConfig{EnableLocal: false, EnableLLM: true},
		&agenttest.FakeLLM{Err: errors.New("provider timeout")},
		"provider", "model", nil, nil,
	)

	result := classifier.Classify(context.Background(), ClassifyInput{Text: "continue", PreviousTaskType: tasktype.Engineering}, nil)
	if result.TaskType != tasktype.Engineering || result.Source != SourcePreviousFallback {
		t.Fatalf("fallback = %+v, want previous engineering fallback", result)
	}
}

type countingDecider struct {
	taskType tasktype.TaskType
	err      error
	calls    *int
}

func (c countingDecider) Decide(context.Context, string) (tasktype.TaskType, error) {
	*c.calls++
	return c.taskType, c.err
}

func TestLocalEngineReplacesLLM(t *testing.T) {
	calls := 0
	classifier := NewDefaultClassifier(
		ClassifierConfig{EnableLocal: false, Engine: EngineLaya},
		nil, "", "",
		countingDecider{taskType: tasktype.Research, calls: &calls}, nil,
	)
	result := classifier.Classify(context.Background(), ClassifyInput{Text: "anything"}, nil)
	if result.TaskType != tasktype.Research || result.Source != SourceLaya {
		t.Fatalf("result = %+v, want research from laya", result)
	}
	if calls != 1 {
		t.Fatalf("decider calls = %d, want 1", calls)
	}
}

func TestLocalEngineFailureUsesFallbackNotLLM(t *testing.T) {
	calls := 0
	classifier := NewDefaultClassifier(
		ClassifierConfig{EnableLocal: false, EnableLLM: true, Engine: EngineLaya},
		&agenttest.FakeLLM{Responses: []string{`{"task_type":"general"}`}},
		"provider", "model",
		countingDecider{err: errors.New("worker died"), calls: &calls}, nil,
	)
	result := classifier.Classify(context.Background(), ClassifyInput{
		Text: "continue", PreviousTaskType: tasktype.Engineering,
	}, nil)
	if result.Source != SourceLayaUnavailable || result.TaskType != tasktype.Engineering {
		t.Fatalf("result = %+v, want previous engineering with laya_unavailable", result)
	}
}

func TestLLMEngineNeverCallsLocalDecider(t *testing.T) {
	calls := 0
	classifier := NewDefaultClassifier(
		ClassifierConfig{EnableLocal: false, EnableLLM: true, Engine: EngineLLM},
		&agenttest.FakeLLM{Responses: []string{`{"task_type":"general"}`}},
		"provider", "model",
		countingDecider{taskType: tasktype.Research, calls: &calls}, nil,
	)
	result := classifier.Classify(context.Background(), ClassifyInput{Text: "hello"}, nil)
	if result.Source != SourceLLM || result.TaskType != tasktype.General {
		t.Fatalf("result = %+v, want general from llm", result)
	}
	if calls != 0 {
		t.Fatalf("decider calls = %d, want 0", calls)
	}
}

type blockingDecider struct{ entered, release chan struct{} }

func (d blockingDecider) Decide(context.Context, string) (tasktype.TaskType, error) {
	close(d.entered)
	<-d.release
	return tasktype.Research, nil
}

func TestEngineSwitchDoesNotWaitForInFlightInference(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	client := &agenttest.FakeLLM{Responses: []string{`{"task_type":"general"}`}}
	c := NewDefaultClassifier(ClassifierConfig{Engine: EngineLaya, EnableLLM: true}, client, "p", "m", blockingDecider{entered, release}, nil)
	r := NewRouter(c, nil, nil)
	result := make(chan ClassificationResult, 1)
	go func() { result <- c.Classify(context.Background(), ClassifyInput{Text: "continue"}, nil) }()
	<-entered
	switched := make(chan struct{})
	go func() { r.UpdateClassifierEngine(EngineLLM); close(switched) }()
	select {
	case <-switched:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("switch waited for inference")
	}
	next := c.Classify(context.Background(), ClassifyInput{Text: "continue"}, nil)
	close(release)
	if previous := <-result; previous.Source != SourceLaya || next.Source != SourceLLM {
		t.Fatalf("previous=%+v next=%+v", previous, next)
	}
}

func TestConcurrentEngineSwitchAndClassify(t *testing.T) {
	client := &agenttest.FakeLLM{Responses: []string{`{"task_type":"general"}`}}
	c := NewDefaultClassifier(ClassifierConfig{EnableLLM: true}, client, "p", "m", nil, nil)
	r := NewRouter(c, nil, nil)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				r.UpdateClassifierEngine(EngineLaya)
				r.UpdateClassifierModel("p", "m")
				r.UpdateClassifierEngine("")
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				result := c.Classify(context.Background(), ClassifyInput{Text: "continue"}, nil)
				if result.Source != SourceLLM && result.Source != SourceLayaUnavailable {
					t.Errorf("unexpected result %+v", result)
				}
			}
		}()
	}
	wg.Wait()
}

//go:embed testdata/chinese_samples.json
var sampleJSON []byte

var routerSamples = func() map[string]string {
	var values map[string]string
	if err := json.Unmarshal(sampleJSON, &values); err != nil {
		panic(err)
	}
	return values
}()

func routerSample(key string) string {
	value, ok := routerSamples[key]
	if !ok {
		panic("missing router test sample: " + key)
	}
	return value
}
