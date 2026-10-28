//go:build !windows

package skill

import (
	"testing"
	"time"
)

func TestExpandShellCommandsKeepsOutputWhenBackgroundHoldsPipe(t *testing.T) {
	got := expandShellCommands("!`(sleep 1; echo late) & echo ready`", PreprocessConfig{ShellTimeout: 5 * time.Second})
	if got != "ready\nlate" {
		t.Fatalf("got %q, want both lines", got)
	}
}
