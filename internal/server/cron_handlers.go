package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/cron"
)

// handleListCronTasks lists all scheduled tasks from SQLite.
func (m *Mux) handleListCronTasks(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	tasks, err := m.toolsCfg.CronStore.ListTasks(r.Context())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tasks == nil {
		tasks = []cron.Task{}
	}
	m.writeJSON(w, http.StatusOK, tasks)
}

// handleCreateCronTask creates a new scheduled task.
func (m *Mux) handleCreateCronTask(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil || m.toolsCfg.CronScheduler == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	var req struct {
		Expression  string `json:"expression"`
		Instruction string `json:"instruction"`
		TargetAgent string `json:"target_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Expression == "" || req.Instruction == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expression and instruction are required"})
		return
	}

	nextRun, err := cron.NextTrigger(req.Expression, time.Now())
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid expression: %v", err)})
		return
	}

	task, err := m.toolsCfg.CronStore.CreateTask(r.Context(), req.Expression, req.Instruction, req.TargetAgent, nextRun)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.toolsCfg.CronScheduler.Schedule(*task)
	m.writeJSON(w, http.StatusCreated, task)
}

// handleUpdateCronTask updates an existing scheduled task (expression, instruction, status).
func (m *Mux) handleUpdateCronTask(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil || m.toolsCfg.CronScheduler == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Expression  string `json:"expression"`
		Instruction string `json:"instruction"`
		TargetAgent string `json:"target_agent"`
		Status      string `json:"status"` // 'active' | 'paused'
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Load existing task
	task, err := m.toolsCfg.CronStore.GetTask(r.Context(), id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Detect changes
	changed := false
	statusChanged := false

	if req.Expression != "" && req.Expression != task.Expression {
		task.Expression = req.Expression
		changed = true
	}
	if req.Instruction != "" && req.Instruction != task.Instruction {
		task.Instruction = req.Instruction
		changed = true
	}
	if req.TargetAgent != "" && req.TargetAgent != task.TargetAgent {
		task.TargetAgent = req.TargetAgent
		changed = true
	}
	if req.Status != "" && req.Status != task.Status {
		task.Status = req.Status
		statusChanged = true
	}

	// Recalculate next run time if expression changed or status changed back to active
	if changed || (statusChanged && task.Status == "active") {
		nextRun, err := cron.NextTrigger(task.Expression, time.Now())
		if err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid expression: %v", err)})
			return
		}
		task.NextRunAt = nextRun
	}

	// Update database
	if err := m.toolsCfg.CronStore.UpdateTask(r.Context(), task); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Dynamically update scheduler
	if task.Status == "active" {
		m.toolsCfg.CronScheduler.Schedule(*task)
	} else {
		m.toolsCfg.CronScheduler.Unschedule(task.ID)
	}

	m.writeJSON(w, http.StatusOK, task)
}

// handleDeleteCronTask deletes a scheduled task.
func (m *Mux) handleDeleteCronTask(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil || m.toolsCfg.CronScheduler == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	id := chi.URLParam(r, "id")

	// Dynamically unschedule
	m.toolsCfg.CronScheduler.Unschedule(id)

	// Delete from database
	if err := m.toolsCfg.CronStore.DeleteTask(r.Context(), id); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}
