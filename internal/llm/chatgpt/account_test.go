package chatgpt

import (
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
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func signedIDToken(t *testing.T, key *rsa.PrivateKey, kid, clientID, nonce string) string {
	t.Helper()
	return signedClaimsToken(t, key, kid, map[string]any{
		"iss": issuer, "sub": "subject", "aud": clientID, "exp": time.Now().Add(time.Hour).Unix(),
		"nonce": nonce, "email": "user@example.test",
	})
}

func signedClaimsToken(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	encodeJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := encodeJSON(map[string]any{"alg": "RS256", "kid": kid})
	claimsPart := encodeJSON(claims)
	input := header + "." + claimsPart
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func oauthTransport(t *testing.T, nonce func() string, beforeToken func()) http.RoundTripper {
	return oauthTransportWithScope(t, nonce, beforeToken, "openid chatgpt.tokens.use.direct")
}

func oauthTransportWithScope(t *testing.T, nonce func() string, beforeToken func(), scope string) http.RoundTripper {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := func(status int, body string) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		switch {
		case strings.HasSuffix(req.URL.Path, "/oauth/token"):
			if beforeToken != nil {
				beforeToken()
			}
			tok := signedIDToken(t, key, "test-key", "client", nonce())
			return response(200, fmt.Sprintf(`{"access_token":"access","refresh_token":"refresh","id_token":%q,"scope":%q,"expires_in":3600}`, tok, scope))
		case strings.HasSuffix(req.URL.Path, "/openid-configuration"):
			return response(200, `{"jwks_uri":"https://auth.openai.com/test-jwks"}`)
		case strings.HasSuffix(req.URL.Path, "/test-jwks"):
			return response(200, fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"test-key","alg":"RS256","n":%q,"e":%q}]}`, enc(key.N.Bytes()), enc(big.NewInt(int64(key.E)).Bytes())))
		default:
			return response(404, `{}`)
		}
	})
}

func startExistingClientLogin(t *testing.T, m *Manager) (state string, a attempt) {
	t.Helper()
	m.cred.ClientID = "client"
	loginURL, err := m.StartLogin("http://127.0.0.1:1455/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	for state, a = range m.pending {
		break
	}
	if state == "" || !strings.Contains(loginURL, "client_id=client") {
		t.Fatal("login attempt was not created")
	}
	return state, a
}

func TestCompletePersistsDynamicRegistrationAndReloadsConnectedStatus(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartLogin("http://127.0.0.1:1455/auth/callback"); err != nil {
		t.Fatal(err)
	}
	var state string
	var a attempt
	for state, a = range m.pending {
		break
	}
	m.client = &http.Client{Transport: oauthTransport(t, func() string { return a.nonce }, nil)}
	if err := m.Complete(context.Background(), state, "code", "client"); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(); !got.Connected || got.Email != "user@example.test" {
		t.Fatalf("status = %+v", got)
	}
	b, err := os.ReadFile(m.credentialPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"client_id": "client"`, `"subject": "subject"`, `"email": "user@example.test"`, `"access_token": "access"`, `"refresh_token": "refresh"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("persisted credentials missing %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), `"scope"`) {
		t.Fatalf("unneeded scope metadata persisted: %s", b)
	}
	reloaded, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Status(); !got.Connected || got.Email != "user@example.test" {
		t.Fatalf("reloaded status = %+v", got)
	}
}

func TestCompleteRejectsReplayExpiryClientMismatchAndMissingScope(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		m, _ := NewManager(t.TempDir())
		state, _ := startExistingClientLogin(t, m)
		if err := m.Complete(context.Background(), state, "", ""); err == nil {
			t.Fatal("empty callback code accepted")
		}
		if err := m.Complete(context.Background(), state, "code", ""); err == nil || !strings.Contains(err.Error(), "unknown or expired") {
			t.Fatalf("replayed callback = %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		m, _ := NewManager(t.TempDir())
		state, a := startExistingClientLogin(t, m)
		a.createdAt = time.Now().Add(-11 * time.Minute)
		m.pending[state] = a
		if err := m.Complete(context.Background(), state, "code", ""); err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("expired callback = %v", err)
		}
	})
	t.Run("client mismatch", func(t *testing.T) {
		m, _ := NewManager(t.TempDir())
		state, _ := startExistingClientLogin(t, m)
		if err := m.Complete(context.Background(), state, "code", "other-client"); err == nil || !strings.Contains(err.Error(), "different") {
			t.Fatalf("mismatched client = %v", err)
		}
	})
	t.Run("missing plan scope", func(t *testing.T) {
		m, _ := NewManager(t.TempDir())
		state, a := startExistingClientLogin(t, m)
		m.client = &http.Client{Transport: oauthTransportWithScope(t, func() string { return a.nonce }, nil, "openid profile")}
		if err := m.Complete(context.Background(), state, "code", ""); err == nil || !strings.Contains(err.Error(), "without permission") {
			t.Fatalf("missing scope = %v", err)
		}
		if m.Status().Connected {
			t.Fatal("missing scope activated credentials")
		}
	})
}

func TestValidateIDTokenRejectsInvalidClaimsAndSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"jwks_uri":"https://auth.openai.com/test-jwks"}`
		if strings.HasSuffix(req.URL.Path, "/test-jwks") {
			body = fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"test-key","alg":"RS256","n":%q,"e":%q}]}`, enc(key.N.Bytes()), enc(big.NewInt(int64(key.E)).Bytes()))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	base := map[string]any{"iss": issuer, "sub": "subject", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce"}
	clone := func() map[string]any {
		out := make(map[string]any, len(base))
		for k, v := range base {
			out[k] = v
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(map[string]any)
		key    *rsa.PrivateKey
	}{
		{name: "issuer", mutate: func(c map[string]any) { c["iss"] = "https://evil.example" }, key: key},
		{name: "audience", mutate: func(c map[string]any) { c["aud"] = "other" }, key: key},
		{name: "nonce", mutate: func(c map[string]any) { c["nonce"] = "other" }, key: key},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }, key: key},
		{name: "signature", mutate: func(map[string]any) {}, key: otherKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := clone()
			tc.mutate(claims)
			token := signedClaimsToken(t, tc.key, "test-key", claims)
			if _, err := validateIDToken(context.Background(), client, token, "client", "nonce"); err == nil {
				t.Fatal("invalid ID token accepted")
			}
		})
	}
	valid := signedClaimsToken(t, key, "test-key", base)
	if claims, err := validateIDToken(context.Background(), client, valid, "client", "nonce"); err != nil || claims.Subject != "subject" {
		t.Fatalf("valid token = %+v, %v", claims, err)
	}
}

func TestCompleteCannotRestoreCredentialsAfterLogout(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, a := startExistingClientLogin(t, m)
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: oauthTransport(t, func() string { return a.nonce }, func() { close(entered); <-release })}
	done := make(chan error, 1)
	go func() { done <- m.Complete(context.Background(), state, "code", "") }()
	<-entered
	if err := m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("Complete() error = %v", err)
	}
	if m.Status().Connected {
		t.Fatal("stale callback restored credentials after logout")
	}
}

func TestOlderCallbackCannotWinAfterReplacementLogin(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, a := startExistingClientLogin(t, m)
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: oauthTransport(t, func() string { return a.nonce }, func() { close(entered); <-release })}
	done := make(chan error, 1)
	go func() { done <- m.Complete(context.Background(), state, "code", "") }()
	<-entered
	if _, err := m.StartLogin("http://127.0.0.1:1455/auth/callback"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("old Complete() error = %v", err)
	}
	if m.Status().Connected {
		t.Fatal("older callback replaced newer login state")
	}
}

func TestCompleteSaveFailureDoesNotActivateCredentials(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	state, a := startExistingClientLogin(t, m)
	m.client = &http.Client{Transport: oauthTransport(t, func() string { return a.nonce }, nil)}
	m.persist = func(Credentials) error { return errors.New("disk full") }
	if err := m.Complete(context.Background(), state, "code", ""); err == nil || !strings.Contains(err.Error(), "save") {
		t.Fatalf("Complete() error = %v", err)
	}
	if m.Status().Connected {
		t.Fatal("unsaved login credentials became active")
	}
	reloaded, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status().Connected {
		t.Fatal("unsaved login credentials survived reload")
	}
}

func tokenEndpointTransport(code string, calls *atomic.Int32, entered, release chan struct{}) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if entered != nil {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
		}
		status, body := 200, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`
		if code != "" {
			status, body = 400, fmt.Sprintf(`{"error":%q}`, code)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
}

func expiredManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.ClientID, m.cred.AccessToken, m.cred.RefreshToken, m.cred.Email = "client", "old-access", "old-refresh", "user@example.test"
	m.cred.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRefreshSaveFailureKeepsDurableOldCredentialsAndRetries(t *testing.T) {
	m := expiredManager(t)
	var calls atomic.Int32
	m.client = &http.Client{Transport: tokenEndpointTransport("", &calls, nil, nil)}
	original := m.cred
	m.persist = func(Credentials) error { return errors.New("disk full") }
	if _, err := m.accessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "save refreshed") {
		t.Fatalf("accessToken() error = %v", err)
	}
	if m.cred != original {
		t.Fatalf("failed save changed in-memory credentials: %+v", m.cred)
	}
	m.persist = m.saveCredentials
	if got, err := m.accessToken(context.Background()); err != nil || got != "new-access" {
		t.Fatalf("retry = %q, %v", got, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("refresh calls = %d, want 2", calls.Load())
	}
}

func TestRefreshWaiterHonorsContextAndRefreshIsSingleFlight(t *testing.T) {
	m := expiredManager(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: tokenEndpointTransport("", &calls, entered, release)}
	leader := make(chan error, 1)
	go func() { _, err := m.accessToken(context.Background()); leader <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := m.accessToken(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("waiter = %v after %s", err, time.Since(start))
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}
	close(release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
}

func TestStartingLoginDoesNotDiscardConcurrentRefresh(t *testing.T) {
	m := expiredManager(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: tokenEndpointTransport("", &calls, entered, release)}
	done := make(chan error, 1)
	go func() {
		_, err := m.accessToken(context.Background())
		done <- err
	}()
	<-entered
	if _, err := m.StartLogin("http://127.0.0.1:1455/auth/callback"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("refresh after StartLogin = %v", err)
	}
	if m.cred.AccessToken != "new-access" || m.cred.RefreshToken != "new-refresh" {
		t.Fatalf("rotated credentials were discarded: %+v", m.cred)
	}
}

func TestRefreshTerminalErrorsClearCredentialsButTemporaryErrorsRetainThem(t *testing.T) {
	terminal := []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"}
	for _, code := range terminal {
		t.Run(code, func(t *testing.T) {
			m := expiredManager(t)
			var calls atomic.Int32
			m.client = &http.Client{Transport: tokenEndpointTransport(code, &calls, nil, nil)}
			if _, err := m.accessToken(context.Background()); err == nil {
				t.Fatal("terminal refresh error was accepted")
			}
			if m.Status().Connected {
				t.Fatal("terminal refresh error retained credentials")
			}
			reloaded, err := NewManager(m.dir)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.Status().Connected {
				t.Fatal("terminal refresh credentials survived reload")
			}
		})
	}
	t.Run("temporary", func(t *testing.T) {
		m := expiredManager(t)
		var calls atomic.Int32
		m.client = &http.Client{Transport: tokenEndpointTransport("temporarily_unavailable", &calls, nil, nil)}
		if _, err := m.accessToken(context.Background()); err == nil {
			t.Fatal("temporary refresh error was accepted")
		}
		if !m.Status().Connected {
			t.Fatal("temporary refresh error cleared credentials")
		}
	})
}

func TestModelsFiltersCatalogAndFallsBackToSlug(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.cred.AccessToken, m.cred.ExpiresAt = "token", time.Now().Add(time.Hour).Unix()
	m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"models":[{"slug":"gpt-a","display_name":"GPT A","visibility":"list"},{"slug":"gpt-b","visibility":"list"},{"slug":"hidden","visibility":"hidden"},{"slug":"","visibility":"list"}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	models, err := m.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Name != "GPT A" || models[1].Name != "gpt-b" {
		t.Fatalf("models = %+v", models)
	}
}

func TestModelsReportsCatalogHTTPAndDecodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "permission", status: http.StatusForbidden, body: `{}`},
		{name: "invalid JSON", status: http.StatusOK, body: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := NewManager(t.TempDir())
			m.cred.AccessToken, m.cred.ExpiresAt = "token", time.Now().Add(time.Hour).Unix()
			m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})}
			if _, err := m.Models(context.Background()); err == nil {
				t.Fatal("catalog error was accepted")
			}
		})
	}
}

func TestLogoutRemoteRevocationAndLocalSaveSemantics(t *testing.T) {
	t.Run("remote success", func(t *testing.T) {
		m := expiredManager(t)
		m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := ""
			if req.Method == http.MethodGet {
				body = `{"revocation_endpoint":"https://auth.openai.com/revoke"}`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}
		if err := m.Logout(context.Background()); err != nil {
			t.Fatal(err)
		}
		if m.Status().Connected {
			t.Fatal("logout left credentials connected")
		}
	})
	t.Run("remote failure still clears local", func(t *testing.T) {
		m := expiredManager(t)
		m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet {
				return nil, errors.New("offline")
			}
			return nil, errors.New("unexpected")
		})}
		if err := m.Logout(context.Background()); err == nil {
			t.Fatal("remote revocation failure was hidden")
		}
		if m.Status().Connected {
			t.Fatal("remote failure prevented local logout")
		}
	})
	t.Run("local save failure retains durable state", func(t *testing.T) {
		m := expiredManager(t)
		m.cred.RefreshToken = ""
		m.persist = func(Credentials) error { return errors.New("disk full") }
		err := m.Logout(context.Background())
		if !errors.Is(err, ErrLocalCredentialClear) {
			t.Fatalf("Logout() error = %v", err)
		}
		if !m.Status().Connected {
			t.Fatal("failed local clear was reported as disconnected")
		}
		reloaded, reloadErr := NewManager(m.dir)
		if reloadErr != nil {
			t.Fatal(reloadErr)
		}
		if !reloaded.Status().Connected {
			t.Fatal("failed local clear changed persisted credentials")
		}
	})
}

func TestCredentialWaiterHonorsContextDuringLogout(t *testing.T) {
	m := expiredManager(t)
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"revocation_endpoint":"https://auth.openai.com/revoke"}`)), Request: req}, nil
		}
		close(entered)
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	logoutDone := make(chan error, 1)
	go func() { logoutDone <- m.Logout(context.Background()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := m.accessToken(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v", err)
	}
	close(release)
	if err := <-logoutDone; err != nil {
		t.Fatal(err)
	}
}

func TestLogoutBlocksNewUsableTokenAndRejectsNewLogin(t *testing.T) {
	m := expiredManager(t)
	m.cred.ExpiresAt = time.Now().Add(time.Hour).Unix()
	entered, release := make(chan struct{}), make(chan struct{})
	m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"revocation_endpoint":"https://auth.openai.com/revoke"}`)), Request: req}, nil
		}
		close(entered)
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	logoutDone := make(chan error, 1)
	go func() { logoutDone <- m.Logout(context.Background()) }()
	<-entered
	if _, err := m.StartLogin("http://127.0.0.1:1455/auth/callback"); err == nil || !strings.Contains(err.Error(), "sign-out") {
		t.Fatalf("StartLogin during logout = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if token, err := m.accessToken(ctx); !errors.Is(err, context.DeadlineExceeded) || token != "" {
		t.Fatalf("token issued during logout: token=%q err=%v", token, err)
	}
	close(release)
	if err := <-logoutDone; err != nil {
		t.Fatal(err)
	}
}
