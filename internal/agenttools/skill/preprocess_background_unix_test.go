//go:build !windows

package skill

import (
	"testing"
	"time"
)

func TestExpandShellCommandsKeepsOutputWhenBackgroundHoldsPipe(t *testing.T) {
	got := expandShellCommands("!`sleep 2 & echo ready`", PreprocessConfig{ShellTimeout: 5 * time.Second})
	if got != "ready" {
		t.Fatalf("got %q, want ready", got)
	}
}
