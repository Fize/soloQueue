package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Helpers ───────────────────────────────────────────────────────────

func startTestServer(t *testing.T, fake *agent.FakeLLM) (*httptest.Server, *session.SessionManager) {
	t.Helper()
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(
			agent.Definition{ID: "test-" + teamID},
			fake,
			nil,
		)
		// agent lifetime must outlive the single Create request ctx;
		// use Background for the agent's parent ctx.
		if err := a.Start(context.Background()); err != nil {
			return nil, nil, nil, err
		}
		cw := ctxwin.NewContextWindow(1048576, 2000, ctxwin.NewTokenizer())
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, 0)
	t.Cleanup(func() { mgr.Shutdown(2 * time.Second) })

	mux := NewMux(mgr, nil)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mgr
}

// createSession is a shortcut: POST /v1/sessions and return the id.
func createSession(t *testing.T, srv *httptest.Server, teamID string) string {
	t.Helper()
	body := strings.NewReader(`{"team_id":"` + teamID + `"}`)
	resp, err := http.Post(srv.URL+"/v1/sessions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /v1/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}
	var out createResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.SessionID
}

// ─── HTTP ──────────────────────────────────────────────────────────────

func TestHTTP_Health(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "ok") {
		t.Errorf("body = %q", b)
	}
}

func TestHTTP_CreateSession(t *testing.T) {
	srv, mgr := startTestServer(t, &agent.FakeLLM{})
	id := createSession(t, srv, "team1")
	if len(id) != 32 {
		t.Errorf("id = %q (len %d), want 32-char hex", id, len(id))
	}
	if mgr.Count() != 1 {
		t.Errorf("mgr count = %d", mgr.Count())
	}
}

func TestHTTP_CreateSession_NoBody(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	req, _ := http.NewRequest("POST", srv.URL+"/v1/sessions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestHTTP_CreateSession_InvalidJSON(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	resp, err := http.Post(srv.URL+"/v1/sessions", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHTTP_DeleteSession(t *testing.T) {
	srv, mgr := startTestServer(t, &agent.FakeLLM{})
	id := createSession(t, srv, "team1")
	req, _ := http.NewRequest("DELETE", srv.URL+"/v1/sessions/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if mgr.Count() != 0 {
		t.Errorf("mgr count = %d", mgr.Count())
	}
}

func TestHTTP_DeleteSession_NotFound(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	req, _ := http.NewRequest("DELETE", srv.URL+"/v1/sessions/ghost", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestHTTP_HistoryEmpty(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{Responses: []string{"r"}})
	id := createSession(t, srv, "team1")
	resp, err := http.Get(srv.URL + "/v1/sessions/" + id + "/history")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	var h historyResp
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if len(h.Messages) != 0 {
		t.Errorf("messages = %v, want empty", h.Messages)
	}
}

func TestHTTP_HistoryAfterAsk(t *testing.T) {
	srv, mgr := startTestServer(t, &agent.FakeLLM{Responses: []string{"hello"}})
	id := createSession(t, srv, "team1")
	// ask via session directly (no WS yet)
	s, _ := mgr.Get(id)
	_, _ = s.Ask(context.Background(), "hi")

	resp, err := http.Get(srv.URL + "/v1/sessions/" + id + "/history")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var h historyResp
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if len(h.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(h.Messages))
	}
	if h.Messages[1].Content != "hello" {
		t.Errorf("assistant = %q", h.Messages[1].Content)
	}
}

func TestHTTP_HistoryNotFound(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	resp, err := http.Get(srv.URL + "/v1/sessions/ghost/history")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// ─── WebSocket ─────────────────────────────────────────────────────────

// wsURL converts httptest server URL to ws://
func wsURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

func dialWS(t *testing.T, srv *httptest.Server, sessionID string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(),
		wsURL(srv.URL, "/v1/sessions/"+sessionID+"/stream"),
		nil,
	)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return c
}

func readFrame(t *testing.T, c *websocket.Conn, timeout time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("ws frame unmarshal: %v (data=%q)", err, data)
	}
	return m
}

func writeFrame(t *testing.T, c *websocket.Conn, frame map[string]any) {
	t.Helper()
	data, _ := json.Marshal(frame)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func TestWS_AskFlow(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"hel", "lo"}}}
	srv, _ := startTestServer(t, fake)
	id := createSession(t, srv, "team1")

	c := dialWS(t, srv, id)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "ask", "prompt": "hi"})

	var collected []map[string]any
	for {
		f := readFrame(t, c, 3*time.Second)
		collected = append(collected, f)
		if f["type"] == "done" {
			break
		}
		if f["type"] == "error" {
			t.Fatalf("got error frame: %v", f)
		}
	}

	// at least: content_delta * 2, iteration_done, done
	types := map[string]int{}
	for _, f := range collected {
		types[f["type"].(string)]++
	}
	if types["content_delta"] < 1 {
		t.Errorf("no content_delta frames; got %v", types)
	}
	if types["done"] != 1 {
		t.Errorf("done count = %d", types["done"])
	}
}

func TestWS_Ping(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	id := createSession(t, srv, "team1")
	c := dialWS(t, srv, id)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "ping"})
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "pong" {
		t.Errorf("frame = %v, want pong", f)
	}
}

func TestWS_UnknownFrameType(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	id := createSession(t, srv, "team1")
	c := dialWS(t, srv, id)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "nonsense"})
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "error" {
		t.Errorf("frame = %v, want error", f)
	}
}

func TestWS_InvalidJSON(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	id := createSession(t, srv, "team1")
	c := dialWS(t, srv, id)
	defer c.Close(websocket.StatusNormalClosure, "")

	_ = c.Write(context.Background(), websocket.MessageText, []byte("{not json"))
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "error" {
		t.Errorf("frame = %v, want error", f)
	}
}

func TestWS_SessionNotFound(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	_, _, err := websocket.Dial(context.Background(),
		wsURL(srv.URL, "/v1/sessions/ghost/stream"),
		nil,
	)
	if err == nil {
		t.Fatal("expected dial error for missing session")
	}
}

func TestWS_Cancel(t *testing.T) {
	// slow stream; cancel mid-flight should stop it
	fake := &agent.FakeLLM{
		StreamDeltas: [][]string{{"a", "b", "c"}},
		Delay:        300 * time.Millisecond,
	}
	srv, _ := startTestServer(t, fake)
	id := createSession(t, srv, "team1")
	c := dialWS(t, srv, id)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "ask", "prompt": "hi"})

	// wait a bit, then send cancel
	time.Sleep(50 * time.Millisecond)
	writeFrame(t, c, map[string]any{"type": "cancel"})

	// 给服务器一点时间处理 cancel 并发送 terminal frame
	time.Sleep(200 * time.Millisecond)

	// drain frames until we see done or error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f := readFrame(t, c, 2*time.Second)
		if f["type"] == "done" || f["type"] == "error" {
			return
		}
	}
	t.Error("never received terminal frame after cancel")
}
