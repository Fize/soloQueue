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
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/session"

	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Helpers ───────────────────────────────────────────────────────────

func startTestServer(t *testing.T, fake *agent.FakeLLM) (*httptest.Server, *session.Session) {
	t.Helper()
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(
			agent.Definition{ID: "test-" + teamID},
			fake,
			nil,
		)
		if err := a.Start(context.Background()); err != nil {
			return nil, nil, nil, err
		}
		cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, nil)
	t.Cleanup(func() { mgr.Shutdown(2 * time.Second) })

	sess, err := mgr.Init(context.Background(), "team1")
	if err != nil {
		t.Fatalf("Init session: %v", err)
	}

	classifier := router.NewDefaultClassifier(router.DefaultClassifierConfig(), nil, "", nil)
	mockModelService := router.NewMockModelService()
	rtr := router.NewRouter(classifier, mockModelService, nil)

	mux := NewMux(sess, rtr, nil)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, sess
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

func TestHTTP_HistoryEmpty(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{Responses: []string{"r"}})
	resp, err := http.Get(srv.URL + "/v1/session/history")
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
	srv, sess := startTestServer(t, &agent.FakeLLM{Responses: []string{"hello"}})
	_, _ = sess.Ask(context.Background(), "hi")

	resp, err := http.Get(srv.URL + "/v1/session/history")
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

// ─── WebSocket ─────────────────────────────────────────────────────────

func wsURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(),
		wsURL(srv.URL, "/v1/session/stream"),
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

	c := dialWS(t, srv)
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
	c := dialWS(t, srv)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "ping"})
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "pong" {
		t.Errorf("frame = %v, want pong", f)
	}
}

func TestWS_UnknownFrameType(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	c := dialWS(t, srv)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "nonsense"})
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "error" {
		t.Errorf("frame = %v, want error", f)
	}
}

func TestWS_InvalidJSON(t *testing.T) {
	srv, _ := startTestServer(t, &agent.FakeLLM{})
	c := dialWS(t, srv)
	defer c.Close(websocket.StatusNormalClosure, "")

	_ = c.Write(context.Background(), websocket.MessageText, []byte("{not json"))
	f := readFrame(t, c, 2*time.Second)
	if f["type"] != "error" {
		t.Errorf("frame = %v, want error", f)
	}
}

func TestWS_Cancel(t *testing.T) {
	fake := &agent.FakeLLM{
		StreamDeltas: [][]string{{"a", "b", "c"}},
		Delay:        300 * time.Millisecond,
	}
	srv, _ := startTestServer(t, fake)
	c := dialWS(t, srv)
	defer c.Close(websocket.StatusNormalClosure, "")

	writeFrame(t, c, map[string]any{"type": "ask", "prompt": "hi"})

	time.Sleep(50 * time.Millisecond)
	writeFrame(t, c, map[string]any{"type": "cancel"})

	time.Sleep(200 * time.Millisecond)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f := readFrame(t, c, 2*time.Second)
		if f["type"] == "done" || f["type"] == "error" {
			return
		}
	}
	t.Error("never received terminal frame after cancel")
}
