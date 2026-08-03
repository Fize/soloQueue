package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

const llmClassifierSystemPrompt = `Classify the user's requested work into exactly one task type. Return ONLY JSON: {"task_type":"general|engineering|research"}.

general: conversation, explanation, writing, translation, summarizing supplied content, ordinary planning.
engineering: code, repositories, debugging, tests, databases, APIs, automation, deployment, or technical implementation.
research: the main goal is external search, current information, source verification, comparisons, or citations.

Difficulty, desired reasoning quality, and image presence never create a task type. Local code/log lookup is engineering; external source lookup is research. If the request follows an established task, preserve that task's type.`

type LLMClassifier struct {
	client agent.LLMClient
	mu sync.RWMutex
	providerID string
	model string
}

func NewLLMClassifier(client agent.LLMClient, providerID, model string) *LLMClassifier {
	return &LLMClassifier{client: client, providerID: providerID, model: model}
}

func (c *LLMClassifier) SetModelAndProvider(providerID, model string) {
	c.mu.Lock()
	c.providerID, c.model = providerID, model
	c.mu.Unlock()
}

func (c *LLMClassifier) Classify(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) (tasktype.TaskType, error) {
	c.mu.RLock()
	providerID, model := c.providerID, c.model
	c.mu.RUnlock()
	classCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	messages := []agent.LLMMessage{{Role: "system", Content: llmClassifierSystemPrompt}}
	for _, msg := range recentHistory(history) {
		messages = append(messages, agent.LLMMessage{Role: msg.Role, Content: msg.Content})
	}
	messages = append(messages,
		agent.LLMMessage{Role: "system", Content: `Input metadata: {"has_images":` + boolJSON(input.HasImages) + `}`},
		agent.LLMMessage{Role: "user", Content: input.Text},
	)
	resp, err := c.client.Chat(classCtx, agent.LLMRequest{
		ProviderID: providerID, Model: model, Temperature: 0, MaxTokens: 64,
		ThinkingEnabled: false, ResponseJSON: true, Messages: messages,
	})
	if err != nil { return tasktype.Unknown, err }
	var result struct { TaskType tasktype.TaskType `json:"task_type"` }
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(resp.Content), "```json"), "```"))
	if err := json.Unmarshal([]byte(content), &result); err != nil { return tasktype.Unknown, err }
	if !result.TaskType.Valid() { return tasktype.Unknown, fmt.Errorf("invalid task type %q", result.TaskType) }
	return result.TaskType, nil
}

func recentHistory(history []ctxwin.PayloadMessage) []ctxwin.PayloadMessage {
	if len(history) > 6 { history = history[len(history)-6:] }
	var out []ctxwin.PayloadMessage
	remaining := 4096
	for i := len(history)-1; i >= 0 && remaining > 0; i-- {
		msg := history[i]
		if msg.Role != "user" && msg.Role != "assistant" { continue }
		if len(msg.Content) > remaining { msg.Content = msg.Content[len(msg.Content)-remaining:] }
		remaining -= len(msg.Content)
		out = append([]ctxwin.PayloadMessage{msg}, out...)
	}
	return out
}

func boolJSON(v bool) string { if v { return "true" }; return "false" }
