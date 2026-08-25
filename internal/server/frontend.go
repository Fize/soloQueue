package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

// NewWebHandler serves only the embedded Web Console. It intentionally has no
// runtime/database dependencies, which makes `soloqueue web` safe to use as a
// static frontend connected to an independently managed backend.
func NewWebHandler(webFS fs.FS, backendURL string) http.Handler {
	files := http.FileServer(http.FS(webFS))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/runtime-config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"backend_url": strings.TrimRight(backendURL, "/")})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if info, err := fs.Stat(webFS, path); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
	return mux
}
