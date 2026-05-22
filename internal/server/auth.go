package server

import (
	"net/http"
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

func (m *Mux) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.authConfig.User == "" {
			next.ServeHTTP(w, r)
			return
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
