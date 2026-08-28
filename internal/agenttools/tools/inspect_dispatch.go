package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

// InspectDelegationTool queries dispatches owned by the current session.
type InspectDelegationTool struct{}

func NewInspectDelegationTool() *InspectDelegationTool { return &InspectDelegationTool{} }

func (*InspectDelegationTool) Name() string { return "inspect_delegation" }
func (*InspectDelegationTool) Description() string {
	return "List delegated work, inspect one dispatch, or read its recent persisted event stream."
}
func (*InspectDelegationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "action":{"type":"string","enum":["list","detail","tail"]},
    "dispatch_id":{"type":"string","description":"Required for detail and tail."},
    "limit":{"type":"integer","minimum":1,"maximum":100,"description":"Recent event count for tail; default 20."}
  },
  "required":["action"]
}`)
}

func (*InspectDelegationTool) Execute(ctx context.Context, args string) (string, error) {
	var input struct {
		Action string `json:"action"`
		ID     string `json:"dispatch_id"`
		Limit  int    `json:"limit"`
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
		value = records
	case "detail":
		record, found := scope.Manager.Get(input.ID)
		if !found || (scope.RootID != "" && record.RootID != scope.RootID) {
			return "", fmt.Errorf("inspect_delegation: %w: %s", os.ErrNotExist, input.ID)
		}
		value = record
	case "tail":
		if input.Limit <= 0 {
			input.Limit = 20
		}
		if input.Limit > 100 {
			input.Limit = 100
		}
		record, found := scope.Manager.Get(input.ID)
		if !found || (scope.RootID != "" && record.RootID != scope.RootID) {
			return "", fmt.Errorf("inspect_delegation: %w: %s", os.ErrNotExist, input.ID)
		}
		events, err := scope.Manager.Tail(input.ID, input.Limit)
		if err != nil {
			return "", fmt.Errorf("inspect_delegation: %w", err)
		}
		value = events
	default:
		return "", errors.New("inspect_delegation: action must be list, detail, or tail")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
