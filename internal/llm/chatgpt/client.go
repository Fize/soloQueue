package chatgpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

type Client struct {
	account       *Manager
	http          *http.Client
	timeout       time.Duration
	mu            sync.Mutex
	continuations map[string][]json.RawMessage
}

func NewClient(account *Manager, timeoutMs int) *Client {
	var timeout time.Duration
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	return &Client{account: account, http: &http.Client{Timeout: 0}, timeout: timeout, continuations: make(map[string][]json.RawMessage)}
}

var _ agent.LLMClient = (*Client)(nil)

type responseRequest struct {
	Model        string             `json:"model"`
	Instructions string             `json:"instructions,omitempty"`
	Input        []any              `json:"input"`
	Tools        []map[string]any   `json:"tools,omitempty"`
	ToolChoice   string             `json:"tool_choice,omitempty"`
	Stream       bool               `json:"stream"`
	Store        bool               `json:"store"`
	Reasoning    *map[string]string `json:"reasoning,omitempty"`
}

func buildRequest(req agent.LLMRequest) responseRequest {
	return buildRequestWithContinuations(req, nil)
}

func buildRequestWithContinuations(req agent.LLMRequest, continuations map[string][]json.RawMessage) responseRequest {
	out, _ := buildRequestAndConsumedContinuations(req, continuations)
	return out
}

func buildRequestAndConsumedContinuations(req agent.LLMRequest, continuations map[string][]json.RawMessage) (responseRequest, []string) {
	out := responseRequest{Model: req.Model, Input: []any{}, Stream: true, Store: false, ToolChoice: req.ToolChoice}
	var instructions []string
	var consumed []string
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if m.Content != "" {
				instructions = append(instructions, m.Content)
			}
		case "tool":
			out.Input = append(out.Input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
		case "assistant":
			var preserved []json.RawMessage
			for _, tc := range m.ToolCalls {
				if items := continuations[tc.ID]; len(items) > 0 {
					if len(preserved) == 0 {
						preserved = items
					}
					consumed = append(consumed, tc.ID)
				}
			}
			if len(preserved) > 0 {
				for _, raw := range preserved {
					var item any
					if json.Unmarshal(raw, &item) == nil {
						out.Input = append(out.Input, item)
					}
				}
			} else {
				if m.Content != "" {
					out.Input = append(out.Input, map[string]any{"role": "assistant", "content": m.Content})
				}
				for _, tc := range m.ToolCalls {
					out.Input = append(out.Input, map[string]any{"type": "function_call", "call_id": tc.ID, "name": tc.Function.Name, "arguments": tc.Function.Arguments})
				}
			}
		case "user":
			if req.Vision && len(m.Images) > 0 {
				content := []any{map[string]any{"type": "input_text", "text": m.Content}}
				for _, img := range m.Images {
					content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + img.MimeType + ";base64," + img.Data})
				}
				out.Input = append(out.Input, map[string]any{"role": "user", "content": content})
			} else {
				out.Input = append(out.Input, map[string]any{"role": "user", "content": m.Content})
			}
		}
	}
	out.Instructions = strings.Join(instructions, "\n\n")
	if len(req.Tools) > 0 {
		functions := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			functions = append(functions, map[string]any{"type": "function", "name": tool.Function.Name, "description": tool.Function.Description, "parameters": nonEmptySchema(tool.Function.Parameters), "strict": false})
		}
		out.Tools = append(out.Tools, map[string]any{"type": "namespace", "name": "soloqueue", "description": "Tools provided by SoloQueue and executed locally.", "tools": functions})
	}
	if req.ThinkingEnabled && req.ReasoningEffort != "" {
		effort := req.ReasoningEffort
		if effort == "max" {
			effort = "high"
		}
		if effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
			effort = "medium"
		}
		out.Reasoning = &map[string]string{"effort": effort, "summary": "auto"}
	}
	return out, consumed
}

func (c *Client) responseRequest(req agent.LLMRequest) (responseRequest, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return buildRequestAndConsumedContinuations(req, c.continuations)
}

func (c *Client) completeContinuation(consumed []string, items []json.RawMessage, callIDs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range consumed {
		delete(c.continuations, id)
	}
	for _, id := range callIDs {
		copyItems := make([]json.RawMessage, len(items))
		for i := range items {
			copyItems[i] = append(json.RawMessage(nil), items[i]...)
		}
		c.continuations[id] = copyItems
	}
	const maxContinuations = 128
	if len(c.continuations) > maxContinuations {
		keys := make([]string, 0, len(c.continuations))
		for id := range c.continuations {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys[:len(keys)-maxContinuations] {
			delete(c.continuations, id)
		}
	}
}
func nonEmptySchema(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return v
}

func (c *Client) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	ch, err := c.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var out agent.LLMResponse
	calls := map[int]*llm.ToolCall{}
	for ev := range ch {
		switch ev.Type {
		case llm.EventDelta:
			out.Content += ev.ContentDelta
			out.ReasoningContent += ev.ReasoningContentDelta
			if d := ev.ToolCallDelta; d != nil {
				t := calls[d.Index]
				if t == nil {
					t = &llm.ToolCall{Type: "function"}
					calls[d.Index] = t
				}
				if d.ID != "" {
					t.ID = d.ID
				}
				if d.Name != "" {
					t.Function.Name = d.Name
				}
				t.Function.Arguments += d.Arguments
			}
		case llm.EventError:
			return nil, ev.Err
		case llm.EventDone:
			out.FinishReason = ev.FinishReason
			if ev.Usage != nil {
				out.Usage = *ev.Usage
			}
			indices := make([]int, 0, len(calls))
			for index := range calls {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			for _, index := range indices {
				out.ToolCalls = append(out.ToolCalls, *calls[index])
			}
			return &out, nil
		}
	}
	return nil, errors.New("chatgpt: stream ended without response.completed")
}

func (c *Client) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	requestCtx := ctx
	var cancelTimeout context.CancelFunc
	if c.timeout > 0 {
		requestCtx, cancelTimeout = context.WithTimeout(ctx, c.timeout)
	}
	cancelOnReturn := true
	defer func() {
		if cancelOnReturn && cancelTimeout != nil {
			cancelTimeout()
		}
	}()
	token, err := c.account.accessToken(requestCtx)
	if err != nil {
		return nil, err
	}
	wire, consumed := c.responseRequest(req)
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("chatgpt: encode Responses request: %w", err)
	}
	hreq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+token)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(hreq)
	if err != nil {
		return nil, errors.New("chatgpt: Responses API request failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&e)
		message := sanitizeProviderMessage(e.Error.Message)
		if message == "" {
			message = "ChatGPT Responses API request failed"
		}
		if requestID := sanitizeProviderMessage(resp.Header.Get("x-request-id")); requestID != "" {
			message += " [request_id: " + requestID + "]"
		}
		return nil, &llm.APIError{StatusCode: resp.StatusCode, Type: e.Error.Type, Code: e.Error.Code, Message: message}
	}
	out := make(chan llm.Event, 16)
	cancelOnReturn = false // the stream goroutine owns the request deadline now
	go func() {
		defer close(out)
		defer resp.Body.Close()
		if cancelTimeout != nil {
			defer cancelTimeout()
		}
		readResponsesWithContinuation(requestCtx, resp.Body, out, func(items []json.RawMessage, callIDs []string) {
			c.completeContinuation(consumed, items, callIDs)
		})
	}()
	return out, nil
}

type sseEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
	Response    struct {
		Status string `json:"status"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"response"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responseOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Arguments string `json:"arguments"`
}

func readResponses(ctx context.Context, r io.Reader, out chan<- llm.Event) {
	readResponsesWithContinuation(ctx, r, out, nil)
}

func readResponsesWithContinuation(ctx context.Context, r io.Reader, out chan<- llm.Event, remember func([]json.RawMessage, []string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	eventName := ""
	var data strings.Builder
	completed := false
	toolCount := 0
	outputItems := make(map[int]json.RawMessage)
	var callIDs []string
	emit := func(e llm.Event) bool {
		select {
		case out <- e:
			return true
		case <-ctx.Done():
			select {
			case out <- llm.Event{Type: llm.EventError, Err: ctx.Err()}:
			default:
			}
			return false
		}
	}
	dispatch := func() bool {
		if data.Len() == 0 {
			eventName = ""
			return true
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(data.String()), &ev); err != nil {
			emit(llm.Event{Type: llm.EventError, Err: fmt.Errorf("chatgpt: invalid Responses event")})
			return false
		}
		typ := ev.Type
		if typ == "" {
			typ = eventName
		}
		data.Reset()
		eventName = ""
		switch typ {
		case "response.output_text.delta", "response.refusal.delta":
			return emit(llm.Event{Type: llm.EventDelta, ContentDelta: ev.Delta})
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			return emit(llm.Event{Type: llm.EventDelta, ReasoningContentDelta: ev.Delta})
		case "response.output_item.added":
			var item responseOutputItem
			_ = json.Unmarshal(ev.Item, &item)
			if item.Type == "function_call" {
				toolCount++
				return emit(llm.Event{Type: llm.EventDelta, ToolCallDelta: &llm.ToolCallDelta{Index: ev.OutputIndex, ID: item.CallID, Name: item.Name, Arguments: item.Arguments}})
			}
		case "response.output_item.done":
			var item responseOutputItem
			if json.Unmarshal(ev.Item, &item) == nil && item.Type != "" {
				outputItems[ev.OutputIndex] = append(json.RawMessage(nil), ev.Item...)
				if item.Type == "function_call" && item.CallID != "" {
					callIDs = append(callIDs, item.CallID)
				}
			}
		case "response.function_call_arguments.delta":
			return emit(llm.Event{Type: llm.EventDelta, ToolCallDelta: &llm.ToolCallDelta{Index: ev.OutputIndex, Arguments: ev.Delta}})
		case "response.failed":
			code, msg := "unknown_error", "ChatGPT response failed"
			if ev.Response.Error != nil {
				if ev.Response.Error.Code != "" {
					code = ev.Response.Error.Code
				}
				if ev.Response.Error.Message != "" {
					msg = sanitizeProviderMessage(ev.Response.Error.Message)
				}
			}
			emit(llm.Event{Type: llm.EventError, Err: &llm.APIError{Code: code, Message: msg}})
			return false
		case "response.incomplete":
			emit(llm.Event{Type: llm.EventError, Err: errors.New("chatgpt: Responses API returned an incomplete response")})
			return false
		case "error":
			code := ev.Code
			if code == "" {
				code = "unknown_error"
			}
			msg := sanitizeProviderMessage(ev.Message)
			if msg == "" {
				msg = "ChatGPT Responses stream returned an error"
			}
			emit(llm.Event{Type: llm.EventError, Err: &llm.APIError{Code: code, Message: msg}})
			return false
		case "response.completed":
			completed = true
			if remember != nil {
				indices := make([]int, 0, len(outputItems))
				for index := range outputItems {
					indices = append(indices, index)
				}
				sort.Ints(indices)
				ordered := make([]json.RawMessage, 0, len(indices))
				for _, index := range indices {
					ordered = append(ordered, outputItems[index])
				}
				remember(ordered, callIDs)
			}
			usage := (*llm.Usage)(nil)
			if ev.Response.Usage != nil {
				usage = &llm.Usage{PromptTokens: ev.Response.Usage.InputTokens, CompletionTokens: ev.Response.Usage.OutputTokens, TotalTokens: ev.Response.Usage.TotalTokens}
			}
			reason := llm.FinishStop
			if toolCount > 0 {
				reason = llm.FinishToolCalls
			}
			emit(llm.Event{Type: llm.EventDone, FinishReason: reason, Usage: usage})
			return false
		}
		return true
	}
	for sc.Scan() {
		if ctx.Err() != nil {
			emit(llm.Event{Type: llm.EventError, Err: ctx.Err()})
			return
		}
		line := sc.Text()
		if line == "" {
			if !dispatch() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := sc.Err(); err != nil {
		emit(llm.Event{Type: llm.EventError, Err: fmt.Errorf("chatgpt: read Responses stream: %w", err)})
		return
	}
	if data.Len() > 0 {
		dispatch()
	}
	if !completed {
		emit(llm.Event{Type: llm.EventError, Err: errors.New("chatgpt: stream ended without response.completed")})
	}
}

func sanitizeProviderMessage(message string) string {
	message = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(message))
	const maxMessageBytes = 2048
	if len(message) > maxMessageBytes {
		message = message[:maxMessageBytes] + "…"
	}
	return message
}

type modelCatalog struct {
	Models []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Visibility  string `json:"visibility"`
	} `json:"models"`
}
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *Manager) Models(ctx context.Context) ([]Model, error) {
	token, err := m.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, errors.New("ChatGPT model catalog request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ChatGPT model catalog returned HTTP %d", resp.StatusCode)
	}
	var c modelCatalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&c); err != nil {
		return nil, errors.New("ChatGPT model catalog response was invalid")
	}
	var out []Model
	for _, v := range c.Models {
		if v.Visibility == "list" && v.Slug != "" {
			name := v.DisplayName
			if name == "" {
				name = v.Slug
			}
			out = append(out, Model{ID: v.Slug, Name: name})
		}
	}
	return out, nil
}
