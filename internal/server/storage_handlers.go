package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/storage"
)

func (m *Mux) storageSnapshot() map[string]any {
	p := m.configSvc.Get().EffectiveStorage()
	last, next := m.storage.Status()
	return map[string]any{"policy": p, "revision": storage.Revision(p), "usage": m.storage.Usage(), "last_cleanup": last, "next_cleanup_at": next, "restart_required": false}
}
func (m *Mux) storageReady(w http.ResponseWriter) bool {
	if m.storage == nil || m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "storage configuration unavailable"})
		return false
	}
	return true
}
func (m *Mux) handleStorage(w http.ResponseWriter, r *http.Request) {
	if !m.storageReady(w) {
		return
	}
	m.writeJSON(w, http.StatusOK, m.storageSnapshot())
}
func readStorageBody(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return &storageBodyError{}
	} else if err != io.EOF {
		return err
	}
	return nil
}

type storageBodyError struct{}

func (*storageBodyError) Error() string { return "only one JSON object is allowed" }
func (m *Mux) handleStoragePreview(w http.ResponseWriter, r *http.Request) {
	if !m.storageReady(w) {
		return
	}
	var req struct {
		Policy        *config.StorageConfig `json:"policy"`
		Revision      string                `json:"revision"`
		IncludeLegacy bool                  `json:"include_legacy"`
	}
	if err := readStorageBody(w, r, &req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid storage preview request"})
		return
	}
	p := m.configSvc.Get().EffectiveStorage()
	if req.Policy != nil {
		p = *req.Policy
	}
	preview, err := m.storage.Preview(p, req.Revision, req.IncludeLegacy)
	if err != nil {
		status := http.StatusBadRequest
		if err == storage.ErrStale {
			status = http.StatusConflict
		}
		m.writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, preview)
}
func (m *Mux) handleStorageUpdate(w http.ResponseWriter, r *http.Request) {
	if !m.storageReady(w) {
		return
	}
	m.storageMu.Lock()
	defer m.storageMu.Unlock()
	var req struct {
		Policy    config.StorageConfig `json:"policy"`
		Revision  string               `json:"revision"`
		PreviewID string               `json:"preview_id"`
	}
	if err := readStorageBody(w, r, &req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid storage policy request"})
		return
	}
	if err := req.Policy.Validate(); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	old := m.configSvc.Get().EffectiveStorage()
	if storage.Revision(old) != req.Revision {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": storage.ErrStale.Error()})
		return
	}
	if storage.Tightens(old, req.Policy) {
		if err := m.storage.ValidatePreview(req.PreviewID, req.Revision, req.Policy); err != nil {
			m.writeJSON(w, http.StatusConflict, map[string]string{"error": "preview the impact before applying a stricter retention policy"})
			return
		}
	}
	if _, err := m.configSvc.Set(func(s *config.Settings) {
		s.Storage = req.Policy
		s.Session.TimelineMaxFileMB = req.Policy.SessionHistory.MaxFileMB
	}); err != nil {
		_, _ = m.configSvc.Set(func(s *config.Settings) { s.Storage = old; s.Session.TimelineMaxFileMB = old.SessionHistory.MaxFileMB })
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save storage policy"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.storageSnapshot())
}
func (m *Mux) handleStorageCleanup(w http.ResponseWriter, r *http.Request) {
	if !m.storageReady(w) {
		return
	}
	m.storageMu.Lock()
	defer m.storageMu.Unlock()
	var req struct {
		PreviewID string `json:"preview_id"`
		Revision  string `json:"revision"`
		Confirm   bool   `json:"confirm"`
	}
	if err := readStorageBody(w, r, &req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cleanup request"})
		return
	}
	result, err := m.storage.Execute(req.PreviewID, req.Revision, req.Confirm)
	if err != nil {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, result)
}
