package server

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/proxy"
)

// GET /api/proxy
func (m *Mux) handleListProxies(w http.ResponseWriter, r *http.Request) {
	if m.proxyManager == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "proxy manager not available"})
		return
	}
	proxies := m.proxyManager.ListProxies()
	if proxies == nil {
		proxies = []proxy.ProxyInfo{}
	}
	m.writeJSON(w, http.StatusOK, proxies)
}

// POST /api/proxy
// Body: {"id": "jenkins", "target_url": "http://localhost:8080"}
func (m *Mux) handleCreateProxy(w http.ResponseWriter, r *http.Request) {
	if m.proxyManager == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "proxy manager not available"})
		return
	}
	var req struct {
		ID        string `json:"id"`
		TargetURL string `json:"target_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.ID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if req.TargetURL == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_url is required"})
		return
	}
	_, err := m.proxyManager.AddProxy(req.ID, req.TargetURL)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         req.ID,
		"target_url": req.TargetURL,
	})
}

// DELETE /api/proxy/{id}
func (m *Mux) handleDeleteProxy(w http.ResponseWriter, r *http.Request) {
	if m.proxyManager == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "proxy manager not available"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := m.proxyManager.RemoveProxy(id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// serveReverseProxy handles forwarding requests to the target URL.
func (m *Mux) serveReverseProxy(w http.ResponseWriter, r *http.Request, id string) {
	if m.proxyManager == nil {
		http.Error(w, "Proxy manager not available", http.StatusServiceUnavailable)
		return
	}

	targetURLStr, healthy, exists := m.proxyManager.GetProxyStatus(id)
	if !exists {
		http.Error(w, "Proxy not found", http.StatusNotFound)
		return
	}
	if !healthy {
		http.Error(w, "Proxy target is unhealthy", http.StatusServiceUnavailable)
		return
	}

	target, err := url.Parse(targetURLStr)
	if err != nil {
		http.Error(w, "Invalid target URL", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if m.log != nil {
			m.log.DebugContext(r.Context(), logger.CatHTTP, "http: proxy error", "err", err.Error(), "url", r.URL.String())
		}
		w.WriteHeader(http.StatusBadGateway)
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		
		req.Host = target.Host
		
		// We no longer strip any path prefix because we use query-param based routing.
		// The path sent to the target is exactly the path requested by the client.
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		// Set a cookie so that WebSocket upgrades (which lack Referer) can identify the active proxy
		cookie := &http.Cookie{
			Name:     "soloqueue_active_proxy",
			Value:    id,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		if v := cookie.String(); v != "" {
			resp.Header.Add("Set-Cookie", v)
		}
		return nil
	}

	proxy.ServeHTTP(w, r)
}
