package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandPrefersNativeWindowsExtensionOverPosixShim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATHEXT", ".EXE;.CMD")
	if err := os.WriteFile(filepath.Join(dir, "server.v1"), []byte("#!/bin/sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "server.v1.exe")
	if err := os.WriteFile(want, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := Command(context.Background(), `.\server.v1`, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != want {
		t.Fatalf("executable = %q, want %q", cmd.Path, want)
	}
}

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

func TestCommandPrefersGlobalWindowsShimOverPosixShim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PATHEXT", ".EXE;.CMD")
	for _, name := range []string{"server.v1", "server.v1.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd, err := Command(context.Background(), "server.v1", nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || !strings.Contains(strings.ToLower(cmd.SysProcAttr.CmdLine), "server.v1.cmd") {
		t.Fatalf("not launched as batch: %#v", cmd)
	}
}

func TestCommandPreservesNonstandardNativePE(t *testing.T) {
	dir := t.TempDir()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "server.v1")
	if err := os.WriteFile(want, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want+".cmd", []byte("@echo off"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := Command(context.Background(), `.\server.v1`, nil, dir)
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

func TestCommandSkipsUnusablePosixShimInEarlierPathDirectory(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	t.Setenv("PATH", first+string(os.PathListSeparator)+second+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PATHEXT", ".EXE;.CMD")
	if err := os.WriteFile(filepath.Join(first, "server.v1"), []byte("#!/bin/sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "server.v1.cmd"), []byte("@echo off"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := Command(context.Background(), "server.v1", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || !strings.Contains(strings.ToLower(cmd.SysProcAttr.CmdLine), strings.ToLower(filepath.Join(second, "server.v1.cmd"))) {
		t.Fatalf("not launched from second directory: %#v", cmd)
	}
}
