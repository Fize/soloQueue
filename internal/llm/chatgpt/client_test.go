package chatgpt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestChatStreamEnforcesConfiguredTimeoutWhileReadingResponse(t *testing.T) {
	const timeout = 75 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	target, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager.cred.AccessToken = "test-token"
	manager.cred.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(manager, int(timeout/time.Millisecond))
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		urlCopy := *req.URL
		urlCopy.Scheme = target.URL.Scheme
		urlCopy.Host = target.URL.Host
		clone.URL = &urlCopy
		return http.DefaultTransport.RoundTrip(clone)
	})}

	started := time.Now()
	stream, err := client.ChatStream(context.Background(), agent.LLMRequest{Model: "gpt-test"})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	select {
	case ev, ok := <-stream:
		elapsed := time.Since(started)
		if !ok || ev.Type != llm.EventError || !errors.Is(ev.Err, context.DeadlineExceeded) {
			t.Fatalf("timeout event = %+v, open=%v; want deadline exceeded", ev, ok)
		}
		if elapsed < timeout/2 {
			t.Fatalf("stream ended after %s, before configured timeout %s", elapsed, timeout)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled response stream did not end at the configured timeout")
	}
}

func TestChatStreamDecodesResponsesAPIErrorTypeAndCode(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager.cred.AccessToken = "test-token"
	manager.cred.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(manager, 0)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Request-Id": []string{"req-test"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"maximum context length exceeded"}}`)),
			Request:    req,
		}, nil
	})}

	_, err = client.ChatStream(context.Background(), agent.LLMRequest{Model: "gpt-test"})
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("ChatStream() error = %v, want *llm.APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Type != "invalid_request_error" || apiErr.Code != "context_length_exceeded" || !strings.Contains(apiErr.Message, "maximum context length") || !strings.Contains(apiErr.Message, "req-test") {
		t.Fatalf("decoded API error = %+v", apiErr)
	}
}

func TestBuildRequestUsesResponsesContractAndMapsHistory(t *testing.T) {
	req := agent.LLMRequest{Model: "gpt-test", Messages: []agent.LLMMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}}}},
		{Role: "tool", ToolCallID: "call-1", Content: "file contents"},
	}, Tools: []llm.ToolDef{{Type: "function", Function: llm.FunctionDecl{Name: "read_file", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}}}}
	wire := buildRequest(req)
	if wire.Model != "gpt-test" || !wire.Stream || wire.Store {
		t.Fatalf("request contract = %+v", wire)
	}
	if wire.Instructions != "instructions" {
		t.Fatalf("instructions = %q", wire.Instructions)
	}
	if len(wire.Input) != 3 {
		t.Fatalf("input items = %d, want 3", len(wire.Input))
	}
	first := wire.Input[0].(map[string]any)
	if first["role"] != "user" {
		t.Fatalf("system instruction leaked into input: %#v", first)
	}
	call := wire.Input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call-1" {
		t.Fatalf("function call history = %#v", call)
	}
	result := wire.Input[2].(map[string]any)
	if result["type"] != "function_call_output" || result["output"] != "file contents" {
		t.Fatalf("function result history = %#v", result)
	}
	if len(wire.Tools) != 1 || wire.Tools[0]["type"] != "namespace" {
		t.Fatalf("tools must be grouped in a namespace: %#v", wire.Tools)
	}
}

func TestBuildRequestPreservesCompletedReasoningItemBeforeToolResult(t *testing.T) {
	req := agent.LLMRequest{Model: "gpt-test", Messages: []agent.LLMMessage{{Role: "assistant", Content: "duplicate assistant text", ReasoningContent: "visible summary", ToolCalls: []llm.ToolCall{{ID: "call-1", Function: llm.FunctionCall{Name: "inspect", Arguments: "{}"}}}}, {Role: "tool", ToolCallID: "call-1", Content: "done"}}}
	wire := buildRequestWithContinuations(req, map[string][]json.RawMessage{"call-1": {
		json.RawMessage(`{"type":"reasoning","id":"rs_1","encrypted_content":"opaque","summary":[]}`),
		json.RawMessage(`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"kept"}]}`),
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call-1","namespace":"soloqueue","name":"inspect","arguments":"{}"}`),
	}})
	if len(wire.Input) != 4 {
		t.Fatalf("input items=%d, want reasoning, message, call, result", len(wire.Input))
	}
	reasoning := wire.Input[0].(map[string]any)
	if reasoning["type"] != "reasoning" || reasoning["id"] != "rs_1" || reasoning["encrypted_content"] != "opaque" {
		t.Fatalf("first item = %#v", reasoning)
	}
	message := wire.Input[1].(map[string]any)
	if message["type"] != "message" || message["id"] != "msg_1" {
		t.Fatalf("second item = %#v", message)
	}
	call := wire.Input[2].(map[string]any)
	if call["namespace"] != "soloqueue" || call["call_id"] != "call-1" {
		t.Fatalf("completed function item = %#v", call)
	}
	b, _ := json.Marshal(wire)
	if bytes.Contains(b, []byte("duplicate assistant text")) {
		t.Fatalf("synthesized assistant message duplicated preserved output: %s", b)
	}
}

func TestChatPreservesRefusalAsAssistantContent(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.AccessToken, m.cred.ExpiresAt = "token", time.Now().Add(time.Hour).Unix()
	c := NewClient(m, 0)
	c.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := strings.Join([]string{
			`event: response.refusal.delta`, `data: {"type":"response.refusal.delta","delta":"I can’t help with that."}`, "",
			`event: response.completed`, `data: {"type":"response.completed","response":{}}`, "",
		}, "\n")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	resp, err := c.Chat(context.Background(), agent.LLMRequest{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "I can’t help with that." {
		t.Fatalf("refusal content = %q", resp.Content)
	}
}

func TestChatTwoTurnsReplaysCompletedReasoningAndFunctionItems(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.AccessToken, m.cred.ExpiresAt = "token", time.Now().Add(time.Hour).Unix()
	c := NewClient(m, 0)
	var requests [][]byte
	c.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		requests = append(requests, body)
		var stream string
		if len(requests) == 1 {
			stream = strings.Join([]string{
				`event: response.output_item.added`, `data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call-1","namespace":"soloqueue","name":"inspect","arguments":"{}"}}`, "",
				`event: response.output_item.done`, `data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call-1","namespace":"soloqueue","name":"inspect","arguments":"{}"}}`, "",
				`event: response.output_item.done`, `data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"calling tool"}]}}`, "",
				`event: response.output_item.done`, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"ciphertext","summary":[]}}`, "",
				`event: response.completed`, `data: {"type":"response.completed","response":{}}`, "",
			}, "\n")
		} else {
			stream = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{}}\n\n"
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(stream)), Request: req}, nil
	})}
	first, err := c.Chat(context.Background(), agent.LLMRequest{Model: "gpt-test", Messages: []agent.LLMMessage{{Role: "user", Content: "inspect"}}})
	if err != nil || len(first.ToolCalls) != 1 {
		t.Fatalf("first turn = %+v, %v", first, err)
	}
	secondReq := agent.LLMRequest{Model: "gpt-test", Messages: []agent.LLMMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: first.ToolCalls},
		{Role: "tool", ToolCallID: "call-1", Content: "ok"},
	}}
	second, err := c.Chat(context.Background(), secondReq)
	if err != nil || second.Content != "done" {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	if !bytes.Contains(requests[1], []byte(`"id":"rs_1"`)) || !bytes.Contains(requests[1], []byte(`"encrypted_content":"ciphertext"`)) || !bytes.Contains(requests[1], []byte(`"namespace":"soloqueue"`)) {
		t.Fatalf("continuation request did not preserve output items: %s", requests[1])
	}
	reasoningAt := bytes.Index(requests[1], []byte(`"id":"rs_1"`))
	messageAt := bytes.Index(requests[1], []byte(`"id":"msg_1"`))
	functionAt := bytes.Index(requests[1], []byte(`"id":"fc_1"`))
	if !(reasoningAt < messageAt && messageAt < functionAt) {
		t.Fatalf("completed output order was not preserved: %s", requests[1])
	}
	if _, ok := c.continuations["call-1"]; ok {
		t.Fatal("successfully consumed continuation was retained")
	}
}

func TestContinuationRetainedOnFailureAndBounded(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.AccessToken, m.cred.ExpiresAt = "token", time.Now().Add(time.Hour).Unix()
	c := NewClient(m, 0)
	c.continuations["call-old"] = []json.RawMessage{json.RawMessage(`{"type":"function_call","call_id":"call-old","name":"inspect","arguments":"{}"}`)}
	c.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"temporary\"}}}\n\n"
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	req := agent.LLMRequest{Model: "gpt-test", Messages: []agent.LLMMessage{{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-old"}}}, {Role: "tool", ToolCallID: "call-old", Content: "result"}}}
	if _, err := c.Chat(context.Background(), req); err == nil {
		t.Fatal("failed continuation request succeeded")
	}
	if _, ok := c.continuations["call-old"]; !ok {
		t.Fatal("failed request consumed continuation needed for retry")
	}
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("call-%03d", i)
		c.completeContinuation(nil, []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"r"}`)}, []string{id})
	}
	if len(c.continuations) != 128 {
		t.Fatalf("continuation entries = %d, want bounded 128", len(c.continuations))
	}
}

func TestReadResponsesRequiresTerminalCompletionAndAccumulatesToolEvents(t *testing.T) {
	body := strings.Join([]string{
		`event: response.output_text.delta`, `data: {"type":"response.output_text.delta","delta":"hello"}`, "",
		`event: response.output_item.added`, `data: {"type":"response.output_item.added","output_index":3,"item":{"type":"function_call","call_id":"call-x","name":"lookup"}}`, "",
		`event: response.function_call_arguments.delta`, `data: {"type":"response.function_call_arguments.delta","output_index":3,"delta":"{}"}`, "",
		`event: response.completed`, `data: {"type":"response.completed","response":{"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`, "",
	}, "\n")
	out := make(chan llm.Event, 8)
	readResponses(context.Background(), strings.NewReader(body), out)
	close(out)
	var content string
	var calls []llm.ToolCall
	var done bool
	for ev := range out {
		switch ev.Type {
		case llm.EventDelta:
			content += ev.ContentDelta
			if ev.ToolCallDelta != nil {
				d := ev.ToolCallDelta
				if len(calls) == 0 {
					calls = append(calls, llm.ToolCall{ID: d.ID, Function: llm.FunctionCall{Name: d.Name}})
				}
				calls[0].Function.Arguments += d.Arguments
			}
		case llm.EventDone:
			done = true
			if ev.FinishReason != llm.FinishToolCalls || ev.Usage == nil || ev.Usage.TotalTokens != 6 {
				t.Fatalf("terminal event = %+v", ev)
			}
		case llm.EventError:
			t.Fatalf("unexpected stream error: %v", ev.Err)
		}
	}
	if !done || content != "hello" || len(calls) != 1 || calls[0].ID != "call-x" || calls[0].Function.Arguments != "{}" {
		t.Fatalf("content=%q calls=%+v done=%v", content, calls, done)
	}
}

func TestReadResponsesRejectsMissingCompletionAndFailure(t *testing.T) {
	for name, body := range map[string]string{
		"missing terminal": `event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n",
		"provider failure": `event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}}` + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			out := make(chan llm.Event, 4)
			readResponses(context.Background(), strings.NewReader(body), out)
			close(out)
			seenError := false
			for ev := range out {
				if ev.Type == llm.EventError {
					seenError = true
				}
			}
			if !seenError {
				t.Fatal("stream was treated as successful without response.completed")
			}
		})
	}
}

func TestStartLoginCreatesPKCEAttemptAndProtectedHostRecord(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	loginURL, err := m.StartLogin("http://127.0.0.1:1455/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loginURL, authorizeURL+"?") {
		t.Fatalf("authorization URL = %s", loginURL)
	}
	var attempt attempt
	for _, a := range m.pending {
		attempt = a
	}
	if attempt.clientID != "dynamic_agent_client" || attempt.verifier == "" || attempt.state == "" || attempt.nonce == "" {
		t.Fatalf("incomplete OAuth attempt: %+v", attempt)
	}
	m.Status()
	if _, ok := m.pending[attempt.state]; !ok {
		t.Fatal("checking connection status invalidated the active OAuth attempt")
	}
	if !hasScope("openid profile email offline_access resource.invoke chatgpt.tokens.use.direct", "chatgpt.tokens.use.direct") {
		t.Fatal("plan-use scope missing")
	}
	info, err := os.Stat(m.credentialPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o, want 600", info.Mode().Perm())
	}
}

func TestGeneratedHostIDIsCanonicalUUIDv4(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "urn:uuid:"
	if !strings.HasPrefix(m.cred.HostID, prefix) {
		t.Fatalf("host ID = %q, want %s prefix", m.cred.HostID, prefix)
	}
	uuid := strings.TrimPrefix(m.cred.HostID, prefix)
	if len(uuid) != 36 || uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		t.Fatalf("UUID shape = %q, want canonical 8-4-4-4-12", uuid)
	}
	if uuid[14] != '4' || !strings.ContainsRune("89ab", rune(uuid[19])) {
		t.Fatalf("UUID version/variant = %q, want RFC 4122 v4", uuid)
	}
}

func TestStatusNeverSerializesCredentials(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.AccessToken = "access-secret"
	m.cred.RefreshToken = "refresh-secret"
	m.cred.IDToken = "id-secret"
	m.cred.Subject = "account-subject"
	m.cred.Email = "user@example.test"
	status, err := json.Marshal(m.Status())
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(status)
	for _, secret := range []string{"access-secret", "refresh-secret", "id-secret", "account-subject"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("status exposed credential: %s", serialized)
		}
	}
	if !strings.Contains(serialized, "user@example.test") {
		t.Fatalf("validated account identity missing: %s", serialized)
	}
}

func TestLogoutClearsAccountDataAndKeepsOnlyProviderRegistration(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.cred.ClientID = "client-id"
	m.cred.Subject = "account-subject"
	m.cred.Email = "user@example.test"
	m.cred.IDToken = "id-token"
	m.cred.AccessToken = "access-token"
	m.cred.RefreshToken = ""
	m.cred.ExpiresAt = time.Now().Add(time.Hour).Unix()

	if err := m.Logout(context.Background()); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	b, err := os.ReadFile(m.credentialPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"subject", "email", "id_token", "access_token", "refresh_token", "expires_at", "scopes"} {
		if _, ok := saved[key]; ok {
			t.Errorf("logout left %q in local credentials: %s", key, b)
		}
	}
	if saved["client_id"] != "client-id" || saved["ext_agent_host_id"] == nil {
		t.Fatalf("logout removed provider registration metadata: %s", b)
	}
}

func TestVerifyJWTSignatureRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	input := "header.payload"
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	encode := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	e := big.NewInt(int64(key.E)).Bytes()
	jwk := struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}{Kty: "RSA", Kid: "k", Alg: "RS256", N: encode(key.N.Bytes()), E: encode(e)}
	if !verifyJWTSignature("RS256", jwk, input, encode(signature)) {
		t.Fatal("valid signature rejected")
	}
	if verifyJWTSignature("HS256", jwk, input, encode(signature)) {
		t.Fatal("unsupported signing algorithm accepted")
	}
}
