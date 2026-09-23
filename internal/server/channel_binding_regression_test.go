package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
	"gopkg.in/yaml.v3"
)

func TestDeleteTelegramBotClearsAgentReferences(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateTelegramBots([]config.TelegramBotConfig{{ID: "telegram-a", Enabled: true, BotToken: "token-a"}}); err != nil {
		t.Fatal(err)
	}
	m := NewMux(workDir, nil, WithConfigService(cfg))
	defer m.Close()
	ctx := context.Background()
	if err := m.teamstore.CreateTeam(ctx, &store.Team{Name: "devs"}); err != nil {
		t.Fatal(err)
	}
	if err := m.teamstore.CreateAgent(ctx, &store.Agent{Name: "research", TeamName: "devs", Channels: map[string]string{"telegram": "telegram-a"}, NotifyChannel: "telegram"}); err != nil {
		t.Fatal(err)
	}
	rolesDir := filepath.Join(workDir, "persona", "roles")
	if err := os.MkdirAll(rolesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rolesDir, "channels.yaml"), []byte("channels:\n  telegram: telegram-a\nnotify_channel: telegram\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := newLocalhostRequest(http.MethodDelete, "/api/config/telegram-bots/telegram-a", nil)
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(cfg.Get().TelegramBots) != 0 {
		t.Fatalf("bot was not deleted: %#v", cfg.Get().TelegramBots)
	}
	agent, err := m.teamstore.GetAgentByName(ctx, "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Channels) != 0 || agent.NotifyChannel != "" {
		t.Fatalf("agent retained deleted channel reference: %#v notify=%q", agent.Channels, agent.NotifyChannel)
	}
	data, err := os.ReadFile(filepath.Join(rolesDir, "channels.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var l1 struct {
		Channels      map[string]string `yaml:"channels"`
		NotifyChannel string            `yaml:"notify_channel"`
	}
	if err := yaml.Unmarshal(data, &l1); err != nil {
		t.Fatal(err)
	}
	if len(l1.Channels) != 0 || l1.NotifyChannel != "" {
		t.Fatalf("L1 retained deleted channel reference: %#v notify=%q", l1.Channels, l1.NotifyChannel)
	}
}

func TestDeleteTeamRejectsChannelBindings(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateTelegramBots([]config.TelegramBotConfig{{ID: "telegram-a", Enabled: true, BotToken: "token-a", BindType: "l2", BindAgent: "devs"}}); err != nil {
		t.Fatal(err)
	}
	m := NewMux(workDir, nil, WithConfigService(cfg))
	defer m.Close()
	ctx := context.Background()
	if err := m.teamstore.CreateTeam(ctx, &store.Team{Name: "devs"}); err != nil {
		t.Fatal(err)
	}
	req := newLocalhostRequest(http.MethodDelete, "/api/teams/devs", nil)
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "channel bindings") {
		t.Fatalf("DELETE status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := m.teamstore.GetTeamByName(ctx, "devs"); err != nil {
		t.Fatalf("team was deleted despite bound channel: %v", err)
	}
}

func TestChannelConfigRejectsIncompleteL2Binding(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMux(workDir, nil, WithConfigService(cfg))
	defer m.Close()
	req := newLocalhostRequest(http.MethodPut, "/api/config/qqbots", strings.NewReader(`[{"id":"qq-a","bind_type":"l2"}]`))
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "bind_agent") {
		t.Fatalf("PUT status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestL1ChannelClearRemovesAnySelectedNotifyChannel(t *testing.T) {
	workDir := t.TempDir()
	rolesDir := filepath.Join(workDir, "persona", "roles")
	if err := os.MkdirAll(rolesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rolesDir, "channels.yaml"), []byte("channels:\n  qq: qq-a\nnotify_channel: qq\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMux(workDir, nil)
	defer m.Close()
	req := newLocalhostRequest(http.MethodPut, "/api/agents/main/profile", strings.NewReader(`{"channels":{}}`))
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(rolesDir, "channels.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "notify_channel: qq") {
		t.Fatalf("stale QQ notify channel remained: %s", data)
	}
}
