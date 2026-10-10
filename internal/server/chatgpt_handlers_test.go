package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func TestChatGPTLoginExposesOnlyLoopbackAuthorizationURL(t *testing.T) {
	mux := NewMux(t.TempDir(), nil, WithRuntimeMetrics(&RuntimeMetrics{HTTPAddr: "127.0.0.1:18929"}))
	req := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/login", nil)
	req.Header.Set("X-SoloQueue-Account-Mutation", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authURL.Query()
	if authURL.Host != "auth.openai.com" || query.Get("client_id") != "dynamic_agent_client" || query.Get("redirect_uri") != "http://127.0.0.1:18929/auth/callback" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected OAuth request parameters: %v", query)
	}
	if !strings.Contains(query.Get("scope"), "chatgpt.tokens.use.direct") || strings.Contains(result.AuthorizationURL, "access_token") || strings.Contains(result.AuthorizationURL, "refresh_token") {
		t.Fatalf("OAuth response did not restrict scope or exposed a credential: %s", rec.Body.String())
	}
}

func TestChatGPTAccountMutationRejectsCrossSiteAndAllowsConfirmedUIRequest(t *testing.T) {
	workDir := t.TempDir()
	authDir := filepath.Join(workDir, "auth", "chatgpt")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credentials := `{"client_id":"client","ext_agent_host_id":"urn:uuid:00000000-0000-4000-8000-000000000000","access_token":"secret","expires_at":4102444800}`
	if err := os.WriteFile(filepath.Join(authDir, "credentials.json"), []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithRuntimeMetrics(&RuntimeMetrics{HTTPAddr: "127.0.0.1:18929"}))
	defer mux.Close()

	foreign := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/logout", strings.NewReader("logout=1"))
	foreign.Header.Set("Origin", "https://evil.example")
	foreign.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, foreign)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign logout status = %d, body=%s", rec.Code, rec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/config/providers/chatgpt/chatgpt/status", nil)
	statusRec := httptest.NewRecorder()
	mux.ServeHTTP(statusRec, statusReq)
	if !strings.Contains(statusRec.Body.String(), `"connected":true`) {
		t.Fatalf("foreign logout changed credentials: %s", statusRec.Body.String())
	}

	legitimate := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/logout", nil)
	legitimate.Header.Set("X-SoloQueue-Account-Mutation", "1")
	legitimateRec := httptest.NewRecorder()
	mux.ServeHTTP(legitimateRec, legitimate)
	if legitimateRec.Code != http.StatusOK {
		t.Fatalf("legitimate logout status = %d, body=%s", legitimateRec.Code, legitimateRec.Body.String())
	}
	statusRec = httptest.NewRecorder()
	mux.ServeHTTP(statusRec, statusReq)
	if !strings.Contains(statusRec.Body.String(), `"connected":false`) {
		t.Fatalf("legitimate logout did not clear credentials: %s", statusRec.Body.String())
	}
}

func TestChatGPTLoginRejectsCrossSiteNavigationBeforeStartingAttempt(t *testing.T) {
	mux := NewMux(t.TempDir(), nil, WithRuntimeMetrics(&RuntimeMetrics{HTTPAddr: "127.0.0.1:18929"}))
	defer mux.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/login", nil)
	req.Header.Set("X-SoloQueue-Account-Mutation", "1")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site login status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatGPTLoginRequiresExplicitMutationHeaderWithoutFetchMetadata(t *testing.T) {
	mux := NewMux(t.TempDir(), nil, WithRuntimeMetrics(&RuntimeMetrics{HTTPAddr: "127.0.0.1:18929"}))
	defer mux.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("headerless login status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatGPTLoginRejectsNonLoopbackListener(t *testing.T) {
	mux := NewMux(t.TempDir(), nil, WithRuntimeMetrics(&RuntimeMetrics{HTTPAddr: "0.0.0.0:18929"}))
	req := httptest.NewRequest(http.MethodPost, "/api/config/providers/chatgpt/chatgpt/login", nil)
	req.Header.Set("X-SoloQueue-Account-Mutation", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("login status = %d, want 503", rec.Code)
	}
}

func TestDeletingChatGPTProviderClearsCredentialsBeforeConfig(t *testing.T) {
	workDir := t.TempDir()
	authDir := filepath.Join(workDir, "auth", "chatgpt")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(authDir, "credentials.json")
	credentials := `{"client_id":"client","ext_agent_host_id":"urn:uuid:00000000-0000-4000-8000-000000000000","access_token":"secret","expires_at":4102444800}`
	if err := os.WriteFile(credentialPath, []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.CreateProvider(config.LLMProvider{ID: "chatgpt", Name: "ChatGPT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithConfigService(cfg))
	defer mux.Close()
	unconfirmed := httptest.NewRequest(http.MethodDelete, "/api/config/providers/chatgpt", nil)
	unconfirmedRec := httptest.NewRecorder()
	mux.ServeHTTP(unconfirmedRec, unconfirmed)
	if unconfirmedRec.Code != http.StatusForbidden || !mux.chatGPTAccount.Status().Connected {
		t.Fatalf("unconfirmed delete status=%d connected=%v", unconfirmedRec.Code, mux.chatGPTAccount.Status().Connected)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/config/providers/chatgpt", nil)
	req.Header.Set("X-SoloQueue-Account-Mutation", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	for _, provider := range cfg.Get().Providers {
		if provider.ID == "chatgpt" {
			t.Fatalf("ChatGPT provider still configured: %+v", cfg.Get().Providers)
		}
	}
	b, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatalf("provider deletion retained credentials: %s", b)
	}
}

func TestDeletingChatGPTProviderKeepsConfigWhenCredentialClearFails(t *testing.T) {
	workDir := t.TempDir()
	authDir := filepath.Join(workDir, "auth", "chatgpt")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(authDir, "credentials.json")
	credentials := `{"client_id":"client","ext_agent_host_id":"urn:uuid:00000000-0000-4000-8000-000000000000","access_token":"secret","expires_at":4102444800}`
	if err := os.WriteFile(credentialPath, []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.CreateProvider(config.LLMProvider{ID: "chatgpt", Name: "ChatGPT", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(workDir, nil, WithConfigService(cfg))
	defer mux.Close()
	if err := os.Remove(credentialPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(credentialPath, 0o700); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/config/providers/chatgpt", nil)
	req.Header.Set("X-SoloQueue-Account-Mutation", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	found := false
	for _, provider := range cfg.Get().Providers {
		found = found || provider.ID == "chatgpt"
	}
	if !found {
		t.Fatalf("provider config deleted despite credential failure: %+v", cfg.Get().Providers)
	}
}
