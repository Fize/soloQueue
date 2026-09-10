package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

func TestTelegramConfigAPIPreservesOtherBotsAndAssignsStableIDs(t *testing.T) {
	workDir := t.TempDir()
	configSvc, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := configSvc.UpdateTelegramBots([]config.TelegramBotConfig{{
		ID: "telegram-1", Name: "Daily", Enabled: true, BotToken: "token-a", BotID: 1001, Username: "daily_bot", BindType: "l1",
	}}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithConfigService(configSvc))
	defer mux.Close()

	body := []byte(`[{"id":"telegram-1","name":"Daily renamed","enabled":true,"bind_type":"l1"},{"name":"Research","enabled":false,"botToken":"token-b","bind_type":"l2","bind_agent":"research"}]`)
	req := newLocalhostRequest(http.MethodPut, "/api/config/telegram-bots/", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	saved := configSvc.Get().TelegramBots
	if len(saved) != 2 {
		t.Fatalf("saved bot count=%d, want 2", len(saved))
	}
	if saved[0].ID != "telegram-1" || saved[0].BotToken != "token-a" || saved[0].BotID != 1001 {
		t.Fatalf("existing bot was not preserved: %#v", saved[0])
	}
	if !strings.HasPrefix(saved[1].ID, "telegram-") || saved[1].ID == "telegram-1" || saved[1].BotToken != "token-b" || saved[1].BindAgent != "research" {
		t.Fatalf("new bot = %#v", saved[1])
	}
	if strings.Contains(recorder.Body.String(), "token-a") || strings.Contains(recorder.Body.String(), "token-b") {
		t.Fatalf("token leaked in response: %s", recorder.Body.String())
	}
}

func TestTelegramIDDoesNotReuseDeletedAccount(t *testing.T) {
	current := []config.TelegramBotConfig{{ID: "telegram-2", BotToken: "fake-existing", BindType: "l1"}}
	bots, err := normalizeTelegramBots([]telegramBotInput{
		{ID: "telegram-2", BindType: "l1"},
		{Name: "New", BotToken: "fake-new", BindType: "l1"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if bots[1].ID == "telegram-1" || bots[1].ID == "telegram-2" {
		t.Fatalf("reused ID %q", bots[1].ID)
	}
	updated, err := normalizeTelegramBots([]telegramBotInput{
		{ID: bots[0].ID, BindType: "l1"}, {ID: bots[1].ID, Name: "Renamed", BindType: "l1"},
	}, bots)
	if err != nil {
		t.Fatal(err)
	}
	if updated[1].ID != bots[1].ID || updated[1].BotToken != "fake-new" {
		t.Fatal("edit changed identity or token")
	}
}

func TestTelegramL1ClearsOldL2Target(t *testing.T) {
	bots, err := normalizeTelegramBots([]telegramBotInput{{ID: "a", BindType: "l1"}},
		[]config.TelegramBotConfig{{ID: "a", BotToken: "fake", BindType: "l2", BindAgent: "devs"}})
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].BindAgent != "" {
		t.Fatal("L1 retained old L2 target")
	}
}

func TestTelegramBindingSaveAndUnbind(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateTelegramBots([]config.TelegramBotConfig{
		{ID: "a", BotToken: "fake-a", Enabled: true, BindType: "l1"},
		{ID: "b", BotToken: "fake-b", Enabled: true, BindType: "l1"},
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
	put("/api/agents/main/profile", `{"channels":{"telegram":"a"},"notify_channel":"telegram"}`, http.StatusOK)
	put("/api/agents/research", `{"channels":{"telegram":"a"}}`, http.StatusBadRequest)
	a, _ := m.teamstore.GetAgentByName(ctx, "research")
	if a.Channels["telegram"] != "" {
		t.Fatal("conflicting selection was saved")
	}
	put("/api/agents/research", `{"channels":{"telegram":"b"},"notify_channel":"telegram"}`, http.StatusOK)
	bots := cfg.Get().TelegramBots
	if bots[0].BindType != "l1" || bots[1].BindType != "l2" || bots[1].BindAgent != "devs" {
		t.Fatal("selection did not update routing to team devs")
	}
	disk, err := cfg.LoadFromDisk()
	if err != nil || disk.TelegramBots[1].BindAgent != "devs" {
		t.Fatal("routing not persisted")
	}
	commits := 0
	cfg.SetOnCommitted(func(config.Settings) { commits++ })
	put("/api/agents/research", `{"description":"Updated description"}`, http.StatusOK)
	put("/api/agents/research", `{"channels":{"telegram":"b"}}`, http.StatusOK)
	put("/api/agents/main/profile", `{"notify_channel":"qq"}`, http.StatusOK)
	if commits != 0 {
		t.Fatal("unchanged routing should not rewrite bot settings or restart gateways")
	}
	put("/api/agents/main/profile", `{"channels":{"telegram":"b"}}`, http.StatusBadRequest)
	put("/api/agents/research", `{"channels":{}}`, http.StatusOK)
	a, _ = m.teamstore.GetAgentByName(ctx, "research")
	if a.Channels["telegram"] != "" || a.NotifyChannel != "" {
		t.Fatal("agent binding or notification not cleared")
	}
	b := cfg.Get().TelegramBots[1]
	if b.Enabled || b.BindAgent != "" {
		t.Fatal("unbound bot remained enabled or retained target")
	}
	put("/api/agents/main/profile", `{"channels":{}}`, http.StatusOK)
	if cfg.Get().TelegramBots[0].Enabled {
		t.Fatal("unbound L1 bot remained enabled")
	}
	put("/api/agents/main/profile", `{"channels":{"telegram":"b"}}`, http.StatusOK)
	b = cfg.Get().TelegramBots[1]
	if b.BindType != "l1" || b.BindAgent != "" {
		t.Fatal("L1 rebind retained L2 routing")
	}
}

func TestTelegramConfigAPIRejectsDuplicateTokenAndClearsChangedIdentity(t *testing.T) {
	workDir := t.TempDir()
	configSvc, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := configSvc.UpdateTelegramBots([]config.TelegramBotConfig{{
		ID: "telegram-1", Name: "Daily", Enabled: true, BotToken: "token-a", BotID: 1001, Username: "daily_bot", BindType: "l1",
	}}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithConfigService(configSvc))
	defer mux.Close()

	duplicate := []byte(`[{"id":"telegram-1","name":"Daily","enabled":true,"bind_type":"l1"},{"id":"telegram-2","name":"Copy","enabled":true,"botToken":"token-a","bind_type":"l1"}]`)
	req := newLocalhostRequest(http.MethodPut, "/api/config/telegram-bots/", bytes.NewReader(duplicate))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "already used") {
		t.Fatalf("duplicate token response status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := configSvc.Get().TelegramBots[0].BotToken; got != "token-a" {
		t.Fatalf("duplicate request changed config to %q", got)
	}

	changed := []byte(`[{"id":"telegram-1","name":"Daily","enabled":true,"bind_type":"l1","botToken":"token-new"}]`)
	req = newLocalhostRequest(http.MethodPut, "/api/config/telegram-bots/", bytes.NewReader(changed))
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("changed token status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	saved := configSvc.Get().TelegramBots[0]
	if saved.BotToken != "token-new" || saved.BotID != 0 || saved.Username != "" {
		t.Fatalf("changed token identity = %#v, want cleared bot identity", saved)
	}

	var response []telegramBotView
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || response[0].Connected || !response[0].CredentialConfigured {
		t.Fatalf("changed token response = %#v", response)
	}
}
