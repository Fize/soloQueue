package runtime

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

func TestStackShutdownClosesRunWatch(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{})
	stack := &Stack{RunWatch: manager}
	stack.Shutdown()

	if _, _, err := manager.Start(context.Background(), runwatch.Metadata{RunID: "after-shutdown"}); err == nil {
		t.Fatal("Start() after Stack.Shutdown() succeeded")
	}
}
