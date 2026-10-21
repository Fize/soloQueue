//go:build integration

package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Each case needs its real language server on PATH. Missing executables are
// reported as skipped, never as evidence that a language works.
func TestOtherLanguagesLazyLoadingIntegration(t *testing.T) {
	for _, test := range []struct {
		name, server, filename, broken, fixed string
		files                                 map[string]string
	}{
		{"Go", "gopls", "main.go", "package main\nvar value int = \"bad\"\nfunc main() {}\n", "package main\nvar value int = 1\nfunc main() {}\n", map[string]string{"go.mod": "module example.com/lspcheck\n\ngo 1.25\n"}},
		{"Python", "pyright", "main.py", "value: int = \"bad\"\n", "value: int = 1\n", map[string]string{"pyrightconfig.json": `{"typeCheckingMode":"basic"}`}},
		{"Bash", "bash", "main.sh", "#!/usr/bin/env bash\nif true; then\n  echo hello\n", "#!/usr/bin/env bash\nif true; then\n  echo hello\nfi\n", nil},
		{"YAML", "yaml", "main.yaml", "name: one\nname: two\n", "name: one\n", nil},
		{"C", "clangd", "main.c", "int main(void) { return missing; }\n", "int main(void) { return 0; }\n", nil},
		{"CPP", "clangd", "main.cpp", "template<class T> T identity(T value) { return value; }\nint main() { return identity(missing); }\n", "template<class T> T identity(T value) { return value; }\nint main() { return identity(1); }\n", nil},
		{"Lua", "lua", "main.lua", "local value = (\n", "local value = 1\n", nil},
		{"Vue", "vue", "main.vue", "<script setup lang=\"ts\">\nconst count: number = \"bad\";\n</script>\n<template><div>{{ count }}</div></template>\n", "<script setup lang=\"ts\">\nconst count: number = 1;\n</script>\n<template><div>{{ count }}</div></template>\n", map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["**/*.vue"]}`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var definition ServerDef
			for _, def := range BuiltinServers() {
				if def.ID == test.server {
					definition = def
				}
			}
			if _, err := exec.LookPath(definition.Command); err != nil {
				t.Skipf("real language server unavailable: %s", definition.Command)
			}
			root := t.TempDir()
			if test.server == "vue" {
				modules := os.Getenv("SOLOQUEUE_LSP_TEST_MODULES")
				if modules == "" {
					t.Skip("set SOLOQUEUE_LSP_TEST_MODULES for the Vue project's dependencies")
				}
				if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
					t.Fatal(err)
				}
			}
			for path, content := range test.files {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(root, test.filename)
			if err := os.WriteFile(file, []byte(test.broken), 0o600); err != nil {
				t.Fatal(err)
			}
			log, err := logger.New(root, logger.WithFile(false))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = log.Close() })
			manager := NewManager(root, log)
			clock := &lifecycleClock{}
			clock.nanos.Store(time.Now().UnixNano())
			manager.now = clock.Now
			t.Cleanup(manager.Shutdown)
			ctx, cancel := context.WithTimeout(iface.ContextWithWorkDir(context.Background(), root), 15*time.Second)
			defer cancel()
			if err := manager.Start(ctx, BuiltinServers()); err != nil {
				t.Fatal(err)
			}
			if len(manager.RunningServerIDs()) != 0 {
				t.Fatal("server started before first use")
			}
			first, cancelFirst := context.WithCancel(ctx)
			client, err := manager.clientForFile(first, file)
			cancelFirst()
			if err != nil {
				t.Fatalf("lazy initialization: %v", err)
			}
			t.Logf("initialized; diagnosticProvider=%v", client.serverCaps.DiagnosticProvider)
			if ids := manager.RunningServerIDs(); len(ids) != 1 || !strings.HasPrefix(ids[0], test.server+"@") {
				t.Fatalf("running servers = %v", ids)
			}
			reused, err := manager.clientForFile(ctx, file)
			if err != nil || reused != client {
				t.Fatal("subsequent use did not reuse the same client")
			}
			t.Log("lazy initialization and subsequent client reuse verified")
			uri := PathToURI(file)
			if err := manager.ensureOpen(client, file, uri); err != nil {
				t.Fatal(err)
			}
			diagnostics, err := client.Diagnostics(ctx, uri)
			if err != nil || len(diagnostics) == 0 {
				t.Fatalf("broken file diagnostics = %v, %v", diagnostics, err)
			}
			t.Logf("broken file: %s", diagnostics[0].Message)
			if err := os.WriteFile(file, []byte(test.fixed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := manager.NotifyFileChanged(file); err != nil {
				t.Fatal(err)
			}
			diagnostics, err = client.Diagnostics(ctx, uri)
			for _, diagnostic := range diagnostics {
				if diagnostic.Severity == 1 {
					t.Errorf("fixed file still has error: %s", diagnostic.Message)
				}
			}
			if err != nil {
				t.Fatalf("fixed file diagnostics: %v", err)
			}
			reused, err = manager.clientForFile(ctx, file)
			if err != nil || reused != client {
				t.Fatal("subsequent use did not reuse the same client")
			}
			select {
			case <-client.done:
				t.Fatal("language server exited after first use")
			default:
			}
			if test.server == "gopls" {
				clock.Advance(10 * time.Minute)
				manager.reapIdle()
				fresh, err := manager.clientForFile(ctx, file)
				if err != nil || fresh == client {
					t.Fatalf("idle reload = %p, %v", fresh, err)
				}
				if err := manager.ensureOpen(fresh, file, uri); err != nil {
					t.Fatal(err)
				}
				if diagnostics, err := fresh.Diagnostics(ctx, uri); err != nil || len(diagnostics) != 0 {
					t.Fatalf("idle reload diagnostics = %v, %v", diagnostics, err)
				}
				if err := fresh.process.Kill(); err != nil {
					t.Fatal(err)
				}
				<-fresh.processDone
				reloaded, err := manager.GetAnyClient(ctx)
				if err != nil || reloaded == fresh {
					t.Fatalf("crash reload = %p, %v", reloaded, err)
				}
				if err := manager.ensureOpen(reloaded, file, uri); err != nil {
					t.Fatal(err)
				}
				if diagnostics, err := reloaded.Diagnostics(ctx, uri); err != nil || len(diagnostics) != 0 {
					t.Fatalf("crash reload diagnostics = %v, %v", diagnostics, err)
				}
			}
		})
	}
}
