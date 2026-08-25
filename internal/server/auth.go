package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"time"
)

const authEnvUser = "SOLOQUEUE_AUTH_USER"
const authEnvPass = "SOLOQUEUE_AUTH_PASSWORD"

type authCheckResponse struct {
	Authenticated bool   `json:"authenticated"`
	User          string `json:"user,omitempty"`
}

type authStatusResponse struct {
	Required      bool   `json:"required"`
	Authenticated bool   `json:"authenticated"`
	Scheme        string `json:"scheme"`
}

// handleAuthStatus is intentionally public so a browser can decide whether to
// render its login gate before making any authenticated API requests. The
// answer is request-specific: localhost is already trusted by the middleware,
// while a remote request must authenticate when credentials are configured.
func (m *Mux) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	local := isLocalhostAccess(r)
	m.writeJSON(w, http.StatusOK, authStatusResponse{
		Required:      m.effectiveAuthUser != "" && !local,
		Authenticated: local || m.hasValidBasicAuth(r),
		Scheme:        "basic",
	})
}

func (m *Mux) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	user := m.effectiveAuthUser
	if user == "" {
		user = "guest"
	}
	m.writeJSON(w, http.StatusOK, authCheckResponse{Authenticated: true, User: user})
}

func (m *Mux) handleGetWSToken(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	m.wsTokens.Range(func(key, val any) bool {
		if expiry, ok := val.(time.Time); ok && now.After(expiry) {
			m.wsTokens.Delete(key)
		}
		return true
	})

	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(bytes)
	m.wsTokens.Store(token, now.Add(30*time.Second))

	m.writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// resolveEffectiveAuth sets m.effectiveAuthUser and m.effectiveAuthPass.
// Priority: settings.yaml [auth] → SOLOQUEUE_AUTH_USER/PASSWORD env vars.
func (m *Mux) resolveEffectiveAuth() {
	// 1. settings.yaml [auth]
	if m.authConfig.User != "" {
		m.effectiveAuthUser = m.authConfig.User
		m.effectiveAuthPass = m.authConfig.Password
		return
	}

	// 2. Environment variables
	envUser := os.Getenv(authEnvUser)
	envPass := os.Getenv(authEnvPass)
	if envUser != "" && envPass != "" {
		m.effectiveAuthUser = envUser
		m.effectiveAuthPass = envPass
		return
	}

	// 3. No credentials configured — auth is disabled.
	// effectiveAuthUser stays empty, no auto-generation happens.
}

// tokenAuthMiddleware enforces Basic Auth for non-localhost requests.
// WebSocket connections may use a one-time ?token= query parameter instead.
func (m *Mux) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Localhost always bypasses auth
		if isLocalhostAccess(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Auth not configured — deny all remote access
		if m.effectiveAuthUser == "" {
			writeLoginFailed(w)
			return
		}

		// These bootstrap endpoints are needed before the browser can make normal
		// authenticated API requests. They are public only after remote access has
		// been configured; auth status validates any supplied credentials itself.
		if r.URL.Path == "/api/auth/status" || r.URL.Path == "/api/runtime-config" {
			next.ServeHTTP(w, r)
			return
		}

		// WebSocket: support one-time query param token
		if r.URL.Path == "/ws" {
			token := r.URL.Query().Get("token")
			if token != "" {
				if expiryVal, ok := m.wsTokens.Load(token); ok {
					if expiry, ok := expiryVal.(time.Time); ok && time.Now().Before(expiry) {
						m.wsTokens.Delete(token) // single use
						next.ServeHTTP(w, r)
						return
					}
				}
			}
		}

		// Basic Auth
		if !m.hasValidBasicAuth(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="SoloQueue"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeLoginFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `{"error":"login failed"}`)
}

func (m *Mux) hasValidBasicAuth(r *http.Request) bool {
	if m.effectiveAuthUser == "" {
		return false
	}
	user, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	userMatches := subtle.ConstantTimeCompare([]byte(user), []byte(m.effectiveAuthUser)) == 1
	passwordMatches := subtle.ConstantTimeCompare([]byte(password), []byte(m.effectiveAuthPass)) == 1
	return userMatches && passwordMatches
}

func isLocalhostAccess(r *http.Request) bool {
	// If Host or RemoteAddr is empty/unset (e.g., in test fixtures), treat as localhost.
	// In production, Go's http.Server always sets RemoteAddr and HTTP/1.1 requires Host.
	if r.Host == "" || r.RemoteAddr == "" {
		return true
	}

	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	if host != "localhost" && host != "127.0.0.1" && host != "[::1]" && host != "::1" {
		return false
	}

	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}
	ip := net.ParseIP(clientIP)
	return ip != nil && ip.IsLoopback()
}

// newLocalhostRequest is a test helper that creates an HTTP request with
// Host and RemoteAddr set to loopback addresses so that isLocalhostAccess
// returns true. Use in tests that don't specifically exercise auth behavior.
func newLocalhostRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1:57647"
	req.RemoteAddr = "127.0.0.1:12345"
	return req
}
