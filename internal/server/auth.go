package server

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"time"
)

type authCheckResponse struct {
	Authenticated bool   `json:"authenticated"`
	User          string `json:"user,omitempty"`
}

func (m *Mux) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	user := m.authConfig.User
	if user == "" {
		user = "guest"
	}
	m.writeJSON(w, http.StatusOK, authCheckResponse{Authenticated: true, User: user})
}

func (m *Mux) handleGetWSToken(w http.ResponseWriter, r *http.Request) {
	// Clean up expired tokens
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

func (m *Mux) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.authConfig.User == "" {
			next.ServeHTTP(w, r)
			return
		}
		if isLocalhostAccess(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Support WebSocket connection using query parameter token
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

		user, password, ok := r.BasicAuth()
		if !ok || user != m.authConfig.User || password != m.authConfig.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="SoloQueue"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalhostAccess(r *http.Request) bool {
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
