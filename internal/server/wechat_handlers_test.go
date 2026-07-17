package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func TestWechatConfigAPIIsRedactedAndPreservesCredentials(t *testing.T) {
	workDir := t.TempDir()
	configSvc, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := configSvc.UpdateWechatBots([]config.WechatBotConfig{{
		ID: "personal", Name: "Personal", Enabled: true,
		BotToken: "secret-token", BotID: "secret-bot-id", BindType: "l1",
	}}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithConfigService(configSvc))
	defer mux.Close()

	req := newLocalhostRequest(http.MethodGet, "/api/config/wechat-bots/", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-token") || strings.Contains(recorder.Body.String(), "secret-bot-id") {
		t.Fatalf("credentials leaked: %s", recorder.Body.String())
	}

	body := []byte(`[{"id":"personal","name":"Renamed","enabled":false,"botToken":"attacker","botId":"replacement","bind_type":"l1"}]`)
	req = newLocalhostRequest(http.MethodPut, "/api/config/wechat-bots/", bytes.NewReader(body))
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	saved := configSvc.Get().WechatBots
	if len(saved) != 1 || saved[0].BotToken != "secret-token" || saved[0].BotID != "secret-bot-id" || saved[0].Name != "Renamed" {
		t.Fatalf("saved config = %#v", saved)
	}
}
