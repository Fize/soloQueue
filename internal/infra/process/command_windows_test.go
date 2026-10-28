package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandPreservesExplicitNativeExtension(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATHEXT", "EXE;CMD")
	want := filepath.Join(dir, "safe.exe")
	for _, file := range []string{want, want + ".cmd"} {
		if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd, err := Command(context.Background(), want, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != want {
		t.Fatalf("executable = %q, want %q", cmd.Path, want)
	}
}

func TestCommandPreservesExplicitBatchEvenWhenExcludedFromPATHEXT(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATHEXT", ".EXE")
	batch := filepath.Join(dir, "tool.cmd")
	for _, path := range []string{batch, batch + ".exe"} {
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd, err := Command(context.Background(), batch, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || !strings.Contains(strings.ToLower(cmd.SysProcAttr.CmdLine), strings.ToLower(batch)) || strings.Contains(strings.ToLower(cmd.SysProcAttr.CmdLine), strings.ToLower(batch+".exe")) {
		t.Fatalf("wrong command: %#v", cmd)
	}
}
