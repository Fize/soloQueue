package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

// InspectDelegationTool queries dispatches owned by the current session.
type InspectDelegationTool struct{}

func NewInspectDelegationTool() *InspectDelegationTool { return &InspectDelegationTool{} }

func (*InspectDelegationTool) Name() string { return "inspect_delegation" }
func (*InspectDelegationTool) Description() string {
	return "List delegated work, inspect one task, or read its recent progress. Use the logical task name; internal runtime identifiers are hidden."
}
func (*InspectDelegationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "action":{"type":"string","enum":["list","detail","tail"]},
	"task_name":{"type":"string","description":"Required for detail and tail; use the logical task_name from the delegation."},
	"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Maximum concise records or recent events to return; defaults to 20 records for list and 10 events for tail."}
  },
  "required":["action"]
}`)
}

const (
	defaultListLimit = 20
	defaultTailLimit = 10
	maxInspectLimit  = 50
	maxSummaryText   = 512
)

// dispatchSummary deliberately omits full task/context text and persistence
// metadata. A follow-up only needs user-meaningful current status.
type dispatchSummary struct {
	TaskName       string          `json:"task_name"`
	Executor       string          `json:"executor"`
	Status         dispatch.Status `json:"status"`
	Phase          string          `json:"phase,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	LastProgressAt time.Time       `json:"last_progress_at,omitempty"`
	Error          string          `json:"-"` // Persisted errors may contain internal paths or identifiers.
}

type dispatchEventSummary struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Dispatch  dispatchSummary `json:"task"`
}

func summarizeDispatch(record dispatch.Record) dispatchSummary {
	return dispatchSummary{
		TaskName:       truncateSummaryText(record.TaskName),
		Executor:       truncateSummaryText(record.Executor),
		Status:         record.Status,
		Phase:          truncateSummaryText(record.Phase),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
		LastProgressAt: record.LastProgressAt,
		Error:          truncateSummaryText(record.Error),
	}
}

func truncateSummaryText(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maxSummaryText {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxSummaryText]) + "…"
}

func boundedLimit(limit, defaultLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxInspectLimit {
		return maxInspectLimit
	}
	return limit
}

func publicEventType(eventType string) string {
	switch {
	case eventType == "created":
		return "started"
	case eventType == string(dispatch.StatusCompleted), eventType == string(dispatch.StatusFailed), eventType == string(dispatch.StatusInterrupted):
		return "finished"
	case strings.HasPrefix(eventType, "agent_event:") || eventType == "progress_checkpoint":
		return "progress"
	case eventType == "cancellation_requested":
		return "cancellation_requested"
	default:
		return "updated"
	}
}

func (*InspectDelegationTool) Execute(ctx context.Context, args string) (string, error) {
	var input struct {
		Action   string `json:"action"`
		TaskName string `json:"task_name"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("inspect_delegation: invalid args: %w", err)
	}
	scope, ok := dispatch.ScopeFromContext(ctx)
	if !ok {
		return "", errors.New("inspect_delegation: no dispatch manager for current session")
	}
	var value any
	switch input.Action {
	case "list":
		records := scope.Manager.List()
		if scope.RootID != "" {
			filtered := records[:0]
			for _, record := range records {
				if record.RootID == scope.RootID {
					filtered = append(filtered, record)
				}
			}
			records = filtered
		}
		limit := boundedLimit(input.Limit, defaultListLimit)
		start := 0
		if len(records) > limit {
			start = len(records) - limit
		}
		summaries := make([]dispatchSummary, 0, len(records)-start)
		for _, record := range records[start:] {
			summaries = append(summaries, summarizeDispatch(record))
		}
		value = struct {
			Dispatches []dispatchSummary `json:"dispatches"`
			Total      int               `json:"total"`
			Truncated  bool              `json:"truncated"`
		}{Dispatches: summaries, Total: len(records), Truncated: start > 0}
	case "detail":
		record, found := scope.Manager.FindByTaskName(input.TaskName, scope.RootID)
		if !found || (scope.RootID != "" && record.RootID != scope.RootID) {
			return "", fmt.Errorf("inspect_delegation: task %q was not found", strings.TrimSpace(input.TaskName))
		}
		value = summarizeDispatch(record)
	case "tail":
		record, found := scope.Manager.FindByTaskName(input.TaskName, scope.RootID)
		if !found || (scope.RootID != "" && record.RootID != scope.RootID) {
			return "", fmt.Errorf("inspect_delegation: task %q was not found", strings.TrimSpace(input.TaskName))
		}
		events, err := scope.Manager.Tail(record.ID, boundedLimit(input.Limit, defaultTailLimit))
		if err != nil {
			return "", errors.New("inspect_delegation: progress unavailable")
		}
		summaries := make([]dispatchEventSummary, 0, len(events))
		for _, event := range events {
			summaries = append(summaries, dispatchEventSummary{
				Timestamp: event.Timestamp, Type: publicEventType(event.Type),
				Dispatch: summarizeDispatch(event.Record),
			})
		}
		value = struct {
			Events []dispatchEventSummary `json:"events"`
			Count  int                    `json:"count"`
		}{Events: summaries, Count: len(summaries)}
	default:
		return "", errors.New("inspect_delegation: action must be list, detail, or tail")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
