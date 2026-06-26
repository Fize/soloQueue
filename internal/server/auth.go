package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

const authEnvUser = "SOLOQUEUE_AUTH_USER"
const authEnvPass = "SOLOQUEUE_AUTH_PASSWORD"

type authCheckResponse struct {
	Authenticated bool   `json:"authenticated"`
	User          string `json:"user,omitempty"`
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
// Priority: settings.toml [auth] → SOLOQUEUE_AUTH_USER/PASSWORD env vars → auto-generated.
func (m *Mux) resolveEffectiveAuth() {
	// 1. settings.toml [auth]
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

	// 3. Auto-generate
	m.effectiveAuthUser = "admin"
	m.effectiveAuthPass = randomHex(16)

	// Always print to stderr so the user sees the credentials at startup,
	// even if the structured logger is not configured.
	os.Stderr.WriteString("\n")
	os.Stderr.WriteString("╔══════════════════════════════════════════════════╗\n")
	os.Stderr.WriteString("║   Remote access credentials (auto-generated)    ║\n")
	os.Stderr.WriteString("╠══════════════════════════════════════════════════╣\n")
	fmt.Fprintf(os.Stderr, "║   User:     %-36s ║\n", m.effectiveAuthUser)
	fmt.Fprintf(os.Stderr, "║   Password: %-36s ║\n", m.effectiveAuthPass)
	os.Stderr.WriteString("╠══════════════════════════════════════════════════╣\n")
	os.Stderr.WriteString("║   Set SOLOQUEUE_AUTH_USER / PASSWORD env vars   ║\n")
	os.Stderr.WriteString("║   or configure [auth] in settings.toml to       ║\n")
	os.Stderr.WriteString("║   use custom credentials.                       ║\n")
	os.Stderr.WriteString("╚══════════════════════════════════════════════════╝\n")
	os.Stderr.WriteString("\n")

	if m.log != nil {
		m.log.Info("remote-auth",
			"status", "auto-generated",
			"user", m.effectiveAuthUser,
			"password", m.effectiveAuthPass,
			"hint", "Set SOLOQUEUE_AUTH_USER and SOLOQUEUE_AUTH_PASSWORD env vars to customize")
	}
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
		user, password, ok := r.BasicAuth()
		if !ok || user != m.effectiveAuthUser || password != m.effectiveAuthPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="SoloQueue"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
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

func randomHex(length int) string {
	bytes := make([]byte, length/2+1)
	if _, err := rand.Read(bytes); err != nil {
		return "change-me"
	}
	return hex.EncodeToString(bytes)[:length]
}
