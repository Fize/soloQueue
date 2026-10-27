package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTypescriptFindsWindowsShim(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "typescript-language-server.cmd")
	if err := os.WriteFile(want, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveTypescript(root); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolvePyrightFindsWindowsVirtualenv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIRTUAL_ENV", "")
	dir := filepath.Join(root, ".venv", "Scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "pyright-langserver.exe")
	if err := os.WriteFile(want, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolvePyright(root); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
