package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ─── Token Store ──────────────────────────────────────────────────────────────

const (
	tokenLen         = 32
	tokenCleanupInt  = 10 * time.Minute
	tokenHardMaxAge = 24 * time.Hour
)

type tokenEntry struct {
	user      string
	createdAt time.Time
	lastUsed  time.Time
}

type tokenStore struct {
	mu     sync.RWMutex
	tokens map[string]tokenEntry
	stopCh chan struct{}
}

func newTokenStore() *tokenStore {
	ts := &tokenStore{
		tokens: make(map[string]tokenEntry),
		stopCh: make(chan struct{}),
	}
	go ts.cleanupLoop()
	return ts
}

func (ts *tokenStore) generateToken(user string) string {
	b := make([]byte, tokenLen)
	rand.Read(b)
	tok := hex.EncodeToString(b)

	ts.mu.Lock()
	ts.tokens[tok] = tokenEntry{
		user:      user,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}
	ts.mu.Unlock()
	return tok
}

func (ts *tokenStore) validateToken(tok string) (string, bool) {
	ts.mu.RLock()
	e, ok := ts.tokens[tok]
	ts.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Since(e.createdAt) > tokenHardMaxAge {
		ts.revokeToken(tok)
		return "", false
	}
	ts.mu.Lock()
	e.lastUsed = time.Now()
	ts.tokens[tok] = e
	ts.mu.Unlock()
	return e.user, true
}

func (ts *tokenStore) revokeToken(tok string) {
	ts.mu.Lock()
	delete(ts.tokens, tok)
	ts.mu.Unlock()
}

func (ts *tokenStore) cleanupLoop() {
	ticker := time.NewTicker(tokenCleanupInt)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ts.cleanup()
		case <-ts.stopCh:
			return
		}
	}
}

func (ts *tokenStore) cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := time.Now()
	for tok, e := range ts.tokens {
		if now.Sub(e.createdAt) > tokenHardMaxAge {
			delete(ts.tokens, tok)
		}
	}
}

func (ts *tokenStore) stop() {
	close(ts.stopCh)
}

// ─── Auth Handlers ────────────────────────────────────────────────────────────

type loginRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
	User  string `json:"user"`
}

func (m *Mux) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		m.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if m.authConfig.User == "" {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "authentication not configured"})
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.User != m.authConfig.User || req.Password != m.authConfig.Password {
		m.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	tok := m.tokenStore.generateToken(req.User)
	m.writeJSON(w, http.StatusOK, loginResponse{Token: tok, User: req.User})
}

type authCheckResponse struct {
	Authenticated bool   `json:"authenticated"`
	User          string `json:"user,omitempty"`
}

func (m *Mux) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	tok := extractBearerToken(r)
	if tok == "" {
		m.writeJSON(w, http.StatusOK, authCheckResponse{Authenticated: false})
		return
	}
	user, ok := m.tokenStore.validateToken(tok)
	if !ok {
		m.writeJSON(w, http.StatusOK, authCheckResponse{Authenticated: false})
		return
	}
	m.writeJSON(w, http.StatusOK, authCheckResponse{Authenticated: true, User: user})
}

func (m *Mux) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		m.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	tok := extractBearerToken(r)
	if tok != "" {
		m.tokenStore.revokeToken(tok)
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Token Auth Middleware ────────────────────────────────────────────────────

func (m *Mux) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.authConfig.User == "" {
			next.ServeHTTP(w, r)
			return
		}
		tok := extractBearerToken(r)
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok == "" {
			m.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		_, ok := m.tokenStore.validateToken(tok)
		if !ok {
			m.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(r *http.Request) string {
	ah := r.Header.Get("Authorization")
	if ah == "" {
		return ""
	}
	if !strings.HasPrefix(ah, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(ah, "Bearer ")
}
