package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel/wecom"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

func TestWecomConfigPreservesSecretAndReportsActualConnection(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateWecomBots([]config.WecomBotConfig{{ID: "w1", BotID: "bot1", Secret: "fake-secret", Name: "one", Enabled: true, BindType: "l1"}}); err != nil {
		t.Fatal(err)
	}
	m := NewMux(dir, nil, WithConfigService(cfg), WithWecomStatus(func(string) wecom.Status { return wecom.Status{State: "connecting"} }))
	defer m.Close()
	body := `[{"id":"w1","botId":"bot1","name":"renamed","enabled":true,"bind_type":"l1"},{"botId":"bot2","secret":"fake-secret2","name":"new","enabled":false,"bind_type":"l1"}]`
	w := httptest.NewRecorder()
	m.ServeHTTP(w, newLocalhostRequest(http.MethodPut, "/api/config/wecom-bots/", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fake-secret") {
		t.Fatal("response leaked Secret")
	}
	bots := cfg.Get().WecomBots
	if len(bots) != 2 || bots[0].Secret != "fake-secret" || !strings.HasPrefix(bots[1].ID, "wecom-") {
		t.Fatal("credentials or IDs not preserved")
	}
	var views []wecomBotView
	if json.Unmarshal(w.Body.Bytes(), &views) != nil || views[0].Connected || views[0].State != "connecting" || !views[0].CredentialConfigured {
		t.Fatalf("views=%s", w.Body.String())
	}
	get := httptest.NewRecorder()
	m.ServeHTTP(get, newLocalhostRequest(http.MethodGet, "/api/config/wecom-bots/", nil))
	if get.Code != 200 || strings.Contains(get.Body.String(), "fake-secret") {
		t.Fatal("GET failed or leaked secret")
	}
}
func TestWecomConfigRejectsDuplicateIdentityAndSecretReuse(t *testing.T) {
	current := []config.WecomBotConfig{{ID: "w1", BotID: "bot1", Secret: "fake", BindType: "l1"}}
	cases := [][]wecomBotInput{
		{{ID: "w1", BotID: "bot2"}},
		{{ID: "w1", BotID: "bot1"}, {ID: "w2", BotID: "bot1", Secret: "fake2"}},
		{{ID: "../bad", BotID: "bot", Secret: "fake"}},
	}
	for _, in := range cases {
		if _, err := normalizeWecomBots(in, current); err == nil {
			t.Fatalf("accepted invalid config %+v", in)
		}
	}
}
func TestWecomBindingRejectsConflictAndDeleteClearsReferences(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = cfg.UpdateWecomBots([]config.WecomBotConfig{{ID: "w1", BotID: "bot1", Secret: "fake", BindType: "l2", BindAgent: "devs"}})
	m := NewMux(dir, nil, WithConfigService(cfg))
	defer m.Close()
	st := m.teamstore
	ctx := context.Background()
	if err := st.CreateTeam(ctx, &store.Team{Name: "devs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgent(ctx, &store.Agent{Name: "worker", TeamName: "devs", Channels: map[string]string{"wecom": "w1"}, NotifyChannel: "wecom"}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.prepareWecomBinding(ctx, "", "", "w1", ""); err == nil {
		t.Fatal("L1 stole L2 binding")
	}
	if err := os.MkdirAll(filepath.Join(dir, "persona", "roles"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "persona", "roles", "channels.yaml"), []byte("channels:\n  wecom: w1\nnotify_channel: wecom\n"), 0644)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, newLocalhostRequest(http.MethodDelete, "/api/config/wecom-bots/w1", nil))
	if w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body.String())
	}
	a, err := st.GetAgentByName(ctx, "worker")
	if err != nil || a.Channels["wecom"] != "" || a.NotifyChannel != "" {
		t.Fatal("L2 binding retained")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "persona", "roles", "channels.yaml"))
	if bytes.Contains(data, []byte("w1")) {
		t.Fatal("L1 binding retained")
	}
	if len(cfg.Get().WecomBots) != 0 {
		t.Fatal("bot retained")
	}
}

func TestWecomBindingSaveAndUnbind(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateWecomBots([]config.WecomBotConfig{
		{ID: "a", BotID: "bot-a", Secret: "fake-a", Enabled: true, BindType: "l1"},
		{ID: "b", BotID: "bot-b", Secret: "fake-b", Enabled: true, BindType: "l1"},
	}); err != nil {
		t.Fatal(err)
	}
	m := NewMux(dir, nil, WithConfigService(cfg))
	defer m.Close()
	ctx := context.Background()
	if err := m.teamstore.CreateTeam(ctx, &store.Team{Name: "devs"}); err != nil {
		t.Fatal(err)
	}
	if err := m.teamstore.CreateAgent(ctx, &store.Agent{Name: "research", TeamName: "devs", IsLeader: true}); err != nil {
		t.Fatal(err)
	}
	put := func(path, body string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, newLocalhostRequest(http.MethodPut, path, strings.NewReader(body)))
		if rec.Code != want {
			t.Fatalf("%s status=%d want=%d: %s", path, rec.Code, want, rec.Body.String())
		}
	}
	put("/api/agents/main/profile", `{"channels":{"wecom":"a"},"notify_channel":"wecom"}`, http.StatusOK)
	put("/api/agents/research", `{"channels":{"wecom":"a"}}`, http.StatusBadRequest)
	a, _ := m.teamstore.GetAgentByName(ctx, "research")
	if a.Channels["wecom"] != "" {
		t.Fatal("conflicting selection was saved")
	}
	put("/api/agents/research", `{"channels":{"wecom":"b"},"notify_channel":"wecom"}`, http.StatusOK)
	bots := cfg.Get().WecomBots
	if bots[0].BindType != "l1" || bots[1].BindType != "l2" || bots[1].BindAgent != "devs" {
		t.Fatal("selection did not update routing to team devs")
	}
	disk, err := cfg.LoadFromDisk()
	if err != nil || disk.WecomBots[1].BindAgent != "devs" {
		t.Fatal("routing not persisted")
	}
	commits := 0
	cfg.SetOnCommitted(func(config.Settings) { commits++ })
	put("/api/agents/research", `{"description":"Updated description"}`, http.StatusOK)
	put("/api/agents/research", `{"channels":{"wecom":"b"}}`, http.StatusOK)
	put("/api/agents/main/profile", `{"notify_channel":"qq"}`, http.StatusOK)
	if commits != 0 {
		t.Fatal("unchanged routing should not rewrite bot settings or restart gateways")
	}
	put("/api/agents/main/profile", `{"channels":{"wecom":"b"}}`, http.StatusBadRequest)
	put("/api/agents/research", `{"channels":{}}`, http.StatusOK)
	a, _ = m.teamstore.GetAgentByName(ctx, "research")
	if a.Channels["wecom"] != "" || a.NotifyChannel != "" {
		t.Fatal("agent binding or notification not cleared")
	}
	b := cfg.Get().WecomBots[1]
	if !b.Enabled || b.BindAgent != "" {
		t.Fatal("unbound bot was disabled or retained target")
	}
	put("/api/agents/main/profile", `{"channels":{}}`, http.StatusOK)
	if !cfg.Get().WecomBots[0].Enabled {
		t.Fatal("unbound L1 bot was disabled")
	}
	put("/api/agents/main/profile", `{"channels":{"wecom":"b"}}`, http.StatusOK)
	b = cfg.Get().WecomBots[1]
	if b.BindType != "l1" || b.BindAgent != "" {
		t.Fatal("L1 rebind retained L2 routing")
	}
}
