package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func TestLocalClassifierOnlyShortCircuitsStrongEvidence(t *testing.T) {
	c := NewLocalClassifier()
	for _, tt := range []struct {
		input   string
		want    tasktype.TaskType
		matched bool
	}{
		{"run go test ./...", tasktype.Engineering, true},
		{"添加一个只读的 resolve_project 工具", tasktype.Engineering, true},
		{"MCP 错误被当成普通文本", tasktype.Engineering, true},
		{"运行中 agent 不会自动更新", tasktype.Engineering, true},
		{"调查为什么这个功能异常", tasktype.Engineering, true},
		{"分析项目里失败的原因", tasktype.Engineering, true},
		{"inspect the router failure", tasktype.Engineering, true},
		{"排查代码库中的问题", tasktype.Engineering, true},
		{"查看最近的日志", tasktype.Engineering, true},
		{"接口响应异常", tasktype.Engineering, true},
		{"提交代码修改", tasktype.Engineering, true},
		{"查询数据库中的慢 SQL", tasktype.Engineering, true},
		{"新增 API 并更新配置", tasktype.Engineering, true},
		{"定位 session 无法恢复的原因", tasktype.Engineering, true},
		{"接入 WebSocket", tasktype.Engineering, true},
		{"升级 LSP", tasktype.Engineering, true},
		{"trace the wire failure", tasktype.Engineering, true},
		{"update the prompt", tasktype.Engineering, true},
		{"优化模型路由", tasktype.Engineering, true},
		{"搜索最新信息并给官方来源", tasktype.Research, true},
		{"翻译下面这段文字", tasktype.General, true},
		{"继续", tasktype.Unknown, false},
		{"设计一个缓存方案", tasktype.Unknown, false},
		{"分析这篇文章的观点", tasktype.Unknown, false},
		{"调查市场规模", tasktype.Unknown, false},
		{"项目里有哪些业务成员", tasktype.Unknown, false},
		{"提交一份总结", tasktype.Unknown, false},
		{"把功能介绍写得更清楚", tasktype.Unknown, false},
		{"优化商业模型", tasktype.Unknown, false},
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
