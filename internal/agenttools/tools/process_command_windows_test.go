package tools

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStartProcessWindowsBatchPreservesStdio(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tool name.cmd")
	if err := os.WriteFile(file, []byte("@echo off\r\necho %~1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: file, Args: []string{"two words"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "two words" {
		t.Fatalf("unexpected stdout %q", data)
	}
}

func TestStartProcessWindowsBatchFromPATH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "test-lsp.cmd"), []byte("@echo off\r\necho path-ready\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: "test-lsp"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "path-ready" {
		t.Fatalf("stdout = %q", output)
	}
}

func TestStartProcessWindowsBatchRejectsShellMetacharacters(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tool.cmd")
	if err := os.WriteFile(file, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: file, Args: []string{"& echo injected"}})
	if err == nil {
		t.Fatal("unsafe batch argument was accepted")
	}
}

func TestStartProcessWindowsBatchResolvesWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relative.cmd"), []byte("@echo off\r\necho relative-ready\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: `.\relative.cmd`, WorkingDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "relative-ready" {
		t.Fatalf("stdout = %q", data)
	}
}

func TestStartProcessWindowsBatchForwardsNodeArguments(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required for the Windows batch integration test")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "node-shim.cmd")
	script := "@echo off\r\nnode -e \"process.stdout.write(JSON.stringify(process.argv.slice(1)))\" %*\r\n"
	if err := os.WriteFile(file, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{`C:\directory with space\`, "plain", ""}
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: file, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode %q: %v", output, err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("arguments = %#v, want %#v", got, args)
	}
}

func TestStartProcessWindowsBatchResolvesExtensionFromWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relative.cmd"), []byte("@echo off\r\necho found-shim\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: `.\relative`, WorkingDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "found-shim" {
		t.Fatalf("stdout = %q", data)
	}
}
