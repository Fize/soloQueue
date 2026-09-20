package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

// CancelDelegationTool requests cancellation of one session-owned delegation.
type CancelDelegationTool struct{}

func NewCancelDelegationTool() *CancelDelegationTool { return &CancelDelegationTool{} }

func (*CancelDelegationTool) Name() string { return "cancel_delegation" }
func (*CancelDelegationTool) Description() string {
	return "Request cancellation of one running delegation and its child work; siblings and the current conversation remain active."
}
func (*CancelDelegationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "task_name":{"type":"string","description":"The logical task_name used when the delegation was started."},
    "reason":{"type":"string","description":"Why the delegation should be stopped."}
  },
  "required":["task_name"]
}`)
}

func (*CancelDelegationTool) Execute(ctx context.Context, args string) (string, error) {
	var input struct {
		TaskName string `json:"task_name"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("cancel_delegation: invalid args: %w", err)
	}
	input.TaskName = strings.TrimSpace(input.TaskName)
	if input.TaskName == "" {
		return "", errors.New("cancel_delegation: task_name is required")
	}
	scope, ok := dispatch.ScopeFromContext(ctx)
	if !ok {
		return "", errors.New("cancel_delegation: no dispatch manager for current session")
	}
	record, found := scope.Manager.FindByTaskName(input.TaskName, scope.RootID)
	if !found || (scope.RootID != "" && record.RootID != scope.RootID) {
		return "", fmt.Errorf("cancel_delegation: task %q was not found", input.TaskName)
	}
	if record.Status != dispatch.StatusRunning {
		return "", fmt.Errorf("cancel_delegation: task %q is already %s", input.TaskName, record.Status)
	}
	if record.Phase == "cancelling" {
		return fmt.Sprintf("cancellation already requested for task %q; inspect_delegation can confirm the terminal state", input.TaskName), nil
	}
	if err := scope.Manager.Cancel(record.ID, input.Reason); err != nil {
		return "", fmt.Errorf("cancel_delegation: task %q could not be cancelled", input.TaskName)
	}
	return fmt.Sprintf("cancellation requested for task %q; inspect_delegation can confirm the terminal state", input.TaskName), nil
}
