package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
	teamstore "github.com/xiaobaitu/soloqueue/internal/team/store"
)

func TestWechatKeepaliveOnlySelectedNotificationRoute(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getconfig" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		calls.Add(1)
		fmt.Fprint(w, `{"ret":0,"typing_ticket":"ticket"}`)
	}))
	defer server.Close()
	manager, sess, _ := newKeepaliveTestManager(t, server.URL, "telegram")
	now := time.Now()
	manager.reconcileWechatKeepalive(context.Background(), now)
	if calls.Load() != 0 {
		t.Fatal("non-WeChat notifications triggered keepalive")
	}
	// Agent definitions are immutable at runtime; this test has no concurrent loop.
	sess.CurrentAgent().Def.NotifyChannel = "wechat"
	manager.reconcileWechatKeepalive(context.Background(), now)
	if calls.Load() != 1 {
		t.Fatalf("startup calls = %d, want 1", calls.Load())
	}
	manager.reconcileWechatKeepalive(context.Background(), now.Add(time.Hour))
	if calls.Load() != 1 {
		t.Fatal("refreshed too soon")
	}
	manager.reconcileWechatKeepalive(context.Background(), now.Add(6*time.Hour))
	if calls.Load() != 2 {
		t.Fatal("idle notification route was not kept alive")
	}
}

func newKeepaliveTestManager(t *testing.T, url, notify string) (*WechatBotManager, *session.Session, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	log, err := logger.New(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mgr := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return &agent.Agent{Def: agent.Definition{NotifyChannel: notify, Channels: map[string]string{"wechat": "logical"}}}, nil, nil, nil
	}, log)
	mgr.SetChannelMetadataStore(database)
	sess, err := mgr.Init(context.Background(), "L1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
	raw, _ := json.Marshal(route)
	if err := database.SaveChannelSenderData("l1-session", "wechat", string(raw)); err != nil {
		t.Fatal(err)
	}
	account := config.WechatBotConfig{ID: "logical", Enabled: true, BotID: "bot", BotToken: "fake", BaseURL: url, BindType: "l1"}
	manager := &WechatBotManager{mgr: mgr, rt: &runtime.Stack{}, workDir: dir, mainLog: log, clients: map[string]*wechat.Client{"bot": wechat.NewClient(wechat.Config{BaseURL: url, Token: "fake"})}, accounts: map[string]config.WechatBotConfig{"bot": account}}
	manager.resetWechatKeepalive()
	t.Cleanup(manager.stopWechatKeepalive)
	return manager, sess, database
}

func TestWechatKeepaliveInboundWinsAndDoesNotDisableNextRefresh(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		fmt.Fprint(w, `{"ret":0,"context_token":"stale-refresh"}`)
	}))
	defer server.Close()
	manager, sess, database := newKeepaliveTestManager(t, server.URL, "wechat")
	now := time.Now()
	done := make(chan struct{})
	go func() { manager.reconcileWechatKeepalive(context.Background(), now); close(done) }()
	<-started
	fresh := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "fresh-inbound"}
	manager.rememberWechatInbound(fresh)
	data, _ := json.Marshal(fresh)
	sess.SetChannelSenderData("wechat", data, func(context.Context, string) error { return nil })
	close(release)
	<-done
	raw, err := database.GetChannelSenderData("l1-session", "wechat")
	if err != nil {
		t.Fatal(err)
	}
	var got channel.Message
	_ = json.Unmarshal([]byte(raw), &got)
	if got.ReplyToken != "fresh-inbound" {
		t.Fatal("late refresh overwrote inbound context")
	}
	manager.reconcileWechatKeepalive(context.Background(), now.Add(7*time.Hour))
	if calls.Load() != 2 {
		t.Fatalf("future refresh calls=%d, want 2", calls.Load())
	}
}

func TestWechatKeepaliveBeforeSendUsesRenewedContextAndFailureStillSends(t *testing.T) {
	for _, tc := range []struct{ name, response, want string }{
		{"new token", `{"ret":0,"context_token":"renewed"}`, "renewed"},
		{"unchanged token", `{"ret":0,"context_token":"old"}`, "old"},
		{"no token", `{"ret":0}`, "old"},
		{"failure", `{"ret":-2}`, "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var configs, sends atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ilink/bot/getconfig":
					configs.Add(1)
					fmt.Fprint(w, tc.response)
				case "/ilink/bot/sendmessage":
					var body struct {
						Msg struct {
							Context string `json:"context_token"`
						} `json:"msg"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Msg.Context != tc.want {
						t.Errorf("sent stale context: got %q want %q", body.Msg.Context, tc.want)
					}
					sends.Add(1)
					fmt.Fprint(w, `{"ret":0}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			manager, _, database := newKeepaliveTestManager(t, server.URL, "wechat")
			ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "logical"})
			original := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
			for i := 0; i < 2; i++ {
				prepared := manager.prepareWechatNotification(ctx, original)
				if err := manager.clients["bot"].SendText(ctx, prepared, "notification"); err != nil {
					t.Fatal(err)
				}
			}
			if configs.Load() != 1 || sends.Load() != 2 {
				t.Fatalf("configs/sends=%d/%d, want 1/2", configs.Load(), sends.Load())
			}
			raw, err := database.GetChannelSenderData("l1-session", "wechat")
			if err != nil {
				t.Fatal(err)
			}
			var persisted channel.Message
			_ = json.Unmarshal([]byte(raw), &persisted)
			if persisted.ReplyToken != tc.want {
				t.Fatal("new token not persisted")
			}
		})
	}
}

func TestWechatKeepaliveNotificationSwitchCancelsPendingAndResumes(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		fmt.Fprint(w, `{"ret":0}`)
	}))
	defer server.Close()
	manager, sess, _ := newKeepaliveTestManager(t, server.URL, "wechat")
	path := filepath.Join(t.TempDir(), "channels.yaml")
	writeSelection := func(selected string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("notify_channel: "+selected+"\nchannels:\n  wechat: logical\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeSelection("wechat")
	sess.SetL1NotifyChannelPath(path)
	done := make(chan struct{})
	go func() { manager.reconcileWechatKeepalive(context.Background(), time.Now()); close(done) }()
	<-started
	writeSelection("telegram")
	manager.reconcileWechatKeepalive(context.Background(), time.Now())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("notification switch did not cancel keepalive")
	}
	<-done
	if calls.Load() != 1 {
		t.Fatal("non-WeChat selection triggered request")
	}
	writeSelection("wechat")
	manager.reconcileWechatKeepalive(context.Background(), time.Now())
	if calls.Load() != 2 {
		t.Fatal("switching back did not resume")
	}
	if err := os.WriteFile(path, []byte("notify_channel: ["), 0600); err != nil {
		t.Fatal(err)
	}
	manager.reconcileWechatKeepalive(context.Background(), time.Now().Add(7*time.Hour))
	if calls.Load() != 2 {
		t.Fatal("invalid config did not fail closed")
	}
}

func TestWechatKeepaliveInactiveL2AndSharedRouteDeduplication(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"ret":0,"context_token":"renewed-group"}`)
	}))
	defer server.Close()
	manager, _, database := newKeepaliveTestManager(t, server.URL, "telegram")
	store := session.NewL2SessionStore(nil, t.TempDir(), manager.mainLog)
	store.SetChannelMetadataStore(database)
	manager.l2Store = store
	manager.accounts["bot"] = config.WechatBotConfig{ID: "logical", Enabled: true, BotID: "bot", BotToken: "fake", BindType: "l2", BindAgent: "research"}
	catalogDir := t.TempDir()
	manager.rt.TeamStore = teamstore.NewStore(filepath.Join(catalogDir, "groups"), filepath.Join(catalogDir, "agents"), database)
	leaders := []teamstore.Agent{
		{Name: "leader-a", IsLeader: true, TeamName: "research", NotifyChannel: "wechat", Channels: map[string]string{"wechat": "logical"}},
		{Name: "leader-b", IsLeader: true, TeamName: "research", NotifyChannel: "wechat", Channels: map[string]string{"wechat": "logical"}},
	}
	for i := range leaders {
		if err := manager.rt.TeamStore.CreateAgent(context.Background(), &leaders[i]); err != nil {
			t.Fatal(err)
		}
	}
	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
	raw, _ := json.Marshal(route)
	if err := database.SaveChannelSenderData("research", "wechat", string(raw)); err != nil {
		t.Fatal(err)
	}
	manager.reconcileWechatKeepalive(context.Background(), time.Now())
	if calls.Load() != 1 {
		t.Fatalf("shared group calls=%d, want 1", calls.Load())
	}
	if len(store.List()) != 0 {
		t.Fatal("keepalive activated an interactive session")
	}
	saved, _ := database.GetChannelSenderData("research", "wechat")
	var got channel.Message
	_ = json.Unmarshal([]byte(saved), &got)
	if got.ReplyToken != "renewed-group" {
		t.Fatal("inactive group token not persisted")
	}
	leaders[0].NotifyChannel = "telegram"
	leaders[1].NotifyChannel = ""
	for i := range leaders {
		if err := manager.rt.TeamStore.UpdateAgent(context.Background(), leaders[i].Name, &leaders[i]); err != nil {
			t.Fatal(err)
		}
	}
	manager.reconcileWechatKeepalive(context.Background(), time.Now().Add(7*time.Hour))
	if calls.Load() != 1 {
		t.Fatal("non-WeChat group kept alive")
	}
}

func TestWechatKeepaliveShutdownCancelsRequest(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	manager, _, _ := newKeepaliveTestManager(t, server.URL, "wechat")
	done := make(chan struct{})
	go func() { manager.runWechatKeepalive(manager.wechatKeeper()); close(done) }()
	<-started
	manager.Shutdown()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel keepalive")
	}
	<-done
}

func TestWechatKeepaliveConcurrentRequestsAreCoalesced(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		fmt.Fprint(w, `{"ret":0,"context_token":"renewed"}`)
	}))
	defer server.Close()
	manager, _, _ := newKeepaliveTestManager(t, server.URL, "wechat")
	ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "logical"})
	msg := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
	result := make(chan channel.Message, 2)
	go func() { result <- manager.prepareWechatNotification(ctx, msg) }()
	<-started
	go func() { result <- manager.prepareWechatNotification(ctx, msg) }()
	close(release)
	for i := 0; i < 2; i++ {
		if got := <-result; got.ReplyToken != "renewed" {
			t.Fatal("concurrent send did not use refreshed context")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d, want 1", calls.Load())
	}
}

func TestWechatKeepaliveDispatchExcludesRoutesNotDue(t *testing.T) {
	manager, _, _ := newKeepaliveTestManager(t, "http://unused.invalid", "wechat")
	k := manager.wechatKeeper()
	now := time.Now()
	source := manager.currentWechatKeepaliveSources(context.Background())[0]
	key := wechatRouteKey(source.msg)
	for _, tc := range []struct {
		name  string
		state wechatKeepaliveRoute
	}{
		{"recent inbound", wechatKeepaliveRoute{activity: now}},
		{"recent keepalive", wechatKeepaliveRoute{checked: now}},
		{"failure backoff", wechatKeepaliveRoute{retryAt: now.Add(time.Minute)}},
		{"inflight", wechatKeepaliveRoute{pending: make(chan struct{})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.state
			state.msg = source.msg
			state.sources = []wechatKeepaliveSource{source}
			k.mu.Lock()
			k.routes[key] = &state
			k.mu.Unlock()
			if got := manager.dueWechatKeepaliveRoutes(k, now); len(got) != 0 {
				t.Fatalf("spawned %d requests for %s", len(got), tc.name)
			}
		})
	}
}

// Tests share the production scheduler's selection and due filtering while
// keeping synchronous execution convenience out of production code.
func (m *WechatBotManager) reconcileWechatKeepalive(ctx context.Context, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	k := m.wechatKeeper()
	for _, key := range m.dueWechatKeepaliveRoutes(k, now) {
		m.refreshWechatContext(ctx, k, key, now)
	}
}

func TestWechatKeepaliveConditionalUpdateConflictUsesCurrentRoute(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"ret":0,"context_token":"stale-refresh"}`)
	}))
	defer server.Close()
	manager, sess, database := newKeepaliveTestManager(t, server.URL, "wechat")
	ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "logical"})
	original := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
	result := make(chan channel.Message, 1)
	go func() { result <- manager.prepareWechatNotification(ctx, original) }()
	<-started
	// Persist a newer route without the manager's inbound observer. A failed CAS
	// must still prevent the old refresh result from becoming the next send context.
	fresh := original
	fresh.ReplyToken = "newer-persisted"
	raw, _ := json.Marshal(fresh)
	sess.SetChannelSenderData("wechat", raw, func(context.Context, string) error { return nil })
	close(release)
	if got := <-result; got.ReplyToken != fresh.ReplyToken {
		t.Fatalf("send context=%q, want current persisted context", got.ReplyToken)
	}
	saved, err := database.GetChannelSenderData("l1-session", "wechat")
	if err != nil {
		t.Fatal(err)
	}
	if saved != string(raw) {
		t.Fatal("new persisted context was overwritten")
	}
}

func TestWechatKeepalivePartialSourceUpdateDoesNotPublishStaleContext(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("write_error_%v", failWrite), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ret":0,"context_token":"refreshed"}`) }))
			defer server.Close()
			manager, sess, database := newKeepaliveTestManager(t, server.URL, "wechat")
			ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "logical"})
			source := manager.currentWechatKeepaliveSources(ctx)[0]
			k := manager.wechatKeeper()
			manager.selectWechatKeepaliveSources(k, []wechatKeepaliveSource{source})
			key := wechatRouteKey(source.msg)
			want := "old"
			failed := source
			failed.save = func(before, after []byte) (bool, error) {
				if failWrite {
					return false, fmt.Errorf("injected route write failure")
				}
				newer := source.msg
				newer.ReplyToken = "new-inbound"
				raw, _ := json.Marshal(newer)
				sess.SetChannelSenderData("wechat", raw, func(context.Context, string) error { return nil })
				return false, nil
			}
			k.mu.Lock()
			if failWrite {
				k.routes[key].sources = []wechatKeepaliveSource{failed}
			} else {
				k.routes[key].sources = []wechatKeepaliveSource{source, failed}
				want = "new-inbound"
			}
			k.mu.Unlock()
			got := manager.refreshWechatContext(ctx, k, key, time.Now(), source.msg)
			if got.ReplyToken != want {
				t.Fatalf("published context=%q, want %q", got.ReplyToken, want)
			}
			stored, err := database.GetChannelSenderData("l1-session", "wechat")
			if err != nil {
				t.Fatal(err)
			}
			var persisted channel.Message
			if err := json.Unmarshal([]byte(stored), &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.ReplyToken != want {
				t.Fatalf("stored context=%q, want %q", persisted.ReplyToken, want)
			}
		})
	}
}

func TestWechatKeepaliveReconciliationCannotRetargetPendingResponse(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"ret":0,"context_token":"response-for-old-context"}`)
	}))
	defer server.Close()
	manager, sess, database := newKeepaliveTestManager(t, server.URL, "wechat")
	ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "logical"})
	original := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "recipient", ReplyToken: "old"}
	result := make(chan channel.Message, 1)
	go func() { result <- manager.prepareWechatNotification(ctx, original) }()
	<-started
	fresh := original
	fresh.ReplyToken = "fresh"
	raw, _ := json.Marshal(fresh)
	sess.SetChannelSenderData("wechat", raw, func(context.Context, string) error { return nil })
	// Reconciliation updates the active state's sources while request A is still
	// blocked. It must never change the metadata against which A may commit.
	manager.reconcileWechatKeepalive(context.Background(), time.Now())
	close(release)
	got := <-result
	if got.ReplyToken != fresh.ReplyToken {
		t.Errorf("waiting notification context=%q, want fresh", got.ReplyToken)
	}
	saved, err := database.GetChannelSenderData("l1-session", "wechat")
	if err != nil {
		t.Fatal(err)
	}
	if saved != string(raw) {
		t.Errorf("reconciliation allowed old response to replace fresh metadata: %s", saved)
	}
}
