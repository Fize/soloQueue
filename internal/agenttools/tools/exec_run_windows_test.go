//go:build windows

package tools

import (
	"context"
	"strings"
	"testing"
)

func TestRunCommand_Windows_EchoPwd(t *testing.T) {
	s := &Sandbox{}
	opts := RunCommandOptions{}
	res, err := s.RunCommand(context.Background(), "echo %cd%", opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Stdout), `\`) {
		t.Errorf("expected Windows path with backslashes, got: %s", string(res.Stdout))
	}
}

func TestRunCommand_Windows_Echo(t *testing.T) {
	s := &Sandbox{}
	opts := RunCommandOptions{}
	res, err := s.RunCommand(context.Background(), "echo hello windows", opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Stdout), "hello windows") {
		t.Errorf("expected 'hello windows' in output, got: %s", string(res.Stdout))
	}
}

func TestRunCommand_Windows_NotFound(t *testing.T) {
	s := &Sandbox{}
	opts := RunCommandOptions{}
	_, err := s.RunCommand(context.Background(), "nonexistentcmd12345", opts)
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestRunCommand_Windows_Cancel(t *testing.T) {
	s := &Sandbox{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts := RunCommandOptions{}
	_, err := s.RunCommand(ctx, "ping -n 60 127.0.0.1", opts)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
