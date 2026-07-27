package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
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
		Title       string `json:"title"`
		TaskType    string `json:"task_type"`
		Expression  string `json:"expression"`
		Instruction string `json:"instruction"`
		TargetAgent string `json:"target_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Title) == "" || req.TaskType == "" || strings.TrimSpace(req.Expression) == "" || strings.TrimSpace(req.Instruction) == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title, task_type, expression, and instruction are required"})
		return
	}
	if err := cron.ValidateTaskTitle(req.Title); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := cron.ValidateTaskType(req.TaskType); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	nextRun, err := cron.NextTrigger(req.Expression, time.Now())
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid expression: %v", err)})
		return
	}

	task, err := m.toolsCfg.CronStore.CreateTask(r.Context(), cron.CreateTaskInput{
		Title: req.Title, TaskType: req.TaskType, Expression: req.Expression,
		Instruction: req.Instruction, TargetAgent: req.TargetAgent, NextRunAt: nextRun,
	})
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
		Title       *string `json:"title"`
		TaskType    *string `json:"task_type"`
		Expression  string  `json:"expression"`
		Instruction string  `json:"instruction"`
		TargetAgent string  `json:"target_agent"`
		Status      string  `json:"status"` // 'active' | 'paused'
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
	if req.Title != nil {
		if err := cron.ValidateTaskTitle(*req.Title); err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		task.Title = *req.Title
		changed = true
	}
	if req.TaskType != nil {
		if err := cron.ValidateTaskType(*req.TaskType); err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		task.TaskType = *req.TaskType
		changed = true
	}

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

// handleListCronHistory lists execution history for a scheduled task.
func (m *Mux) handleListCronHistory(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	id := chi.URLParam(r, "id")

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	records, err := m.toolsCfg.CronStore.ListExecutionHistory(r.Context(), id, limit, offset)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if records == nil {
		records = []cron.ExecutionRecord{}
	}
	m.writeJSON(w, http.StatusOK, records)
}

// handleGetCronHistory returns the full timeline events for a specific execution.
func (m *Mux) handleGetCronHistory(w http.ResponseWriter, r *http.Request) {
	if m.toolsCfg == nil || m.toolsCfg.CronStore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cron system not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	execID := chi.URLParam(r, "execID")

	rec, err := m.toolsCfg.CronStore.GetExecutionHistory(r.Context(), id, execID)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Read timeline events only when the execution actually produced a timeline.
	events := []timeline.Event{}
	if strings.TrimSpace(rec.TimelineDir) != "" {
		timelineDir := rec.TimelineDir
		if !strings.HasPrefix(timelineDir, "/") {
			timelineDir = m.workDir + "/" + timelineDir
		}
		if loaded, err := readAllTimelineEvents(timelineDir); err == nil {
			events = loaded
		}
	}

	m.writeJSON(w, http.StatusOK, map[string]interface{}{
		"execution": rec,
		"events":    events,
	})
}
