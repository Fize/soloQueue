//go:build integration

package lsp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// SOLOQUEUE_LSP_TEST_MODULES points to an isolated node_modules containing Vue,
// React, their types, TypeScript and both language servers. No installation is
// performed by the test, and each project owns only its source/config files.
func frameworkProject(t *testing.T, files map[string]string) (*Manager, context.Context, string, *lifecycleClock) {
	t.Helper()
	modules := os.Getenv("SOLOQUEUE_LSP_TEST_MODULES")
	if modules == "" {
		t.Skip("set SOLOQUEUE_LSP_TEST_MODULES to run real framework integration tests")
	}
	root := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log, err := logger.New(root, logger.WithFile(false), logger.WithConsole(os.Getenv("SOLOQUEUE_LSP_TEST_DEBUG") != ""), logger.WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	manager := NewManager(root, log)
	clock := &lifecycleClock{}
	clock.nanos.Store(time.Now().UnixNano())
	manager.now = clock.Now
	t.Cleanup(manager.Shutdown)
	ctx, cancel := context.WithTimeout(iface.ContextWithWorkDir(context.Background(), root), 20*time.Second)
	t.Cleanup(cancel)
	if err := manager.Start(ctx, BuiltinServers()); err != nil {
		t.Fatal(err)
	}
	if len(manager.RunningServerIDs()) != 0 {
		t.Fatal("framework service started before first use")
	}
	return manager, ctx, root, clock
}

func TestVueFrameworkIntegration(t *testing.T) {
	broken := "<script setup lang=\"ts\">\nimport { ref } from 'vue';\nconst count = ref<number>(\"bad\");\nfunction increment() { count.value++; }\n</script>\n<template><button @click=\"increment\">{{ count.toUpperCase() }}</button></template>\n"
	fixed := strings.Replace(broken, `ref<number>("bad")`, `ref<number>(1)`, 1)
	fixed = strings.Replace(fixed, "count.toUpperCase()", "count.toFixed(0)", 1)
	manager, ctx, root, clock := frameworkProject(t, map[string]string{
		"App.vue":       broken,
		"tsconfig.json": `{"compilerOptions":{"strict":true,"target":"ES2022","module":"ESNext","moduleResolution":"Bundler","skipLibCheck":true},"include":["**/*.vue"]}`,
	})
	file := filepath.Join(root, "App.vue")
	client, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	uri := PathToURI(file)
	if err := manager.ensureOpen(client, file, uri); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := client.Diagnostics(ctx, uri)
	found := false
	templateError := false
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "not assignable") {
			found = true
		}
		if diagnostic.Range.Start.Line == 5 && strings.Contains(diagnostic.Message, "toUpperCase") {
			templateError = true
		}
	}
	if err != nil || !found || !templateError {
		t.Fatalf("Vue script type error missing: %v, %v", diagnostics, err)
	}
	template := strings.Split(broken, "\n")[5]
	hover, err := client.Hover(ctx, uri, Position{Line: 5, Character: strings.Index(template, "count")})
	hoverJSON, _ := json.Marshal(hover)
	if err != nil || hover == nil || !strings.Contains(string(hoverJSON), "number") {
		t.Fatalf("Vue template hover = %v, %v", hover, err)
	}
	locations, err := client.GotoDefinition(ctx, uri, Position{Line: 5, Character: strings.Index(template, "increment")})
	if err != nil || len(locations) == 0 || locations[0].Range.Start.Line != 3 {
		t.Fatalf("Vue template definition = %v, %v", locations, err)
	}
	references, err := client.FindReferences(ctx, uri, Position{Line: 3, Character: strings.Index(strings.Split(broken, "\n")[3], "increment")}, true)
	if err != nil || len(references) < 2 {
		t.Fatalf("Vue script/template references = %v, %v", references, err)
	}
	rename, err := client.Rename(ctx, uri, Position{Line: 3, Character: strings.Index(strings.Split(broken, "\n")[3], "increment")}, "increase")
	if err != nil || rename == nil {
		t.Fatalf("Vue rename: %v", err)
	}
	edits := 0
	for _, changes := range rename.Changes {
		edits += len(changes)
	}
	if edits < 2 {
		t.Fatalf("Vue rename did not include script and template: %v", rename)
	}
	if err := os.WriteFile(file, []byte(fixed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.NotifyFileChanged(file); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []string{"edit", "idle", "crash", "typescript-crash"} {
		previous := client
		if cause == "idle" {
			clock.Advance(10 * time.Minute)
			manager.reapIdle()
		}
		if cause == "crash" {
			if err := client.process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-client.processDone
		}
		if cause == "typescript-crash" {
			if err := client.vue.typescript.process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-client.vue.typescript.processDone
		}
		client, err = manager.clientForFile(ctx, file)
		if err != nil {
			t.Fatal(err)
		}
		if cause != "edit" && client == previous {
			t.Fatalf("Vue %s reused stale process", cause)
		}
		if cause != "edit" {
			select {
			case <-previous.processDone:
			default:
				t.Fatal("Vue group retained old parent process")
			}
			select {
			case <-previous.vue.typescript.processDone:
			default:
				t.Fatal("Vue group retained old TypeScript process")
			}
		}
		if err := manager.ensureOpen(client, file, uri); err != nil {
			t.Fatal(err)
		}
		diagnostics, err := client.Diagnostics(ctx, uri)
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == 1 {
				t.Errorf("Vue %s: %s", cause, diagnostic.Message)
			}
		}
		if err != nil {
			t.Fatalf("Vue %s diagnostics: %v", cause, err)
		}
	}
}

func TestReactFrameworkIntegration(t *testing.T) {
	for _, extension := range []string{".jsx", ".tsx"} {
		t.Run(extension, func(t *testing.T) {
			parameter := "props"
			if extension == ".tsx" {
				parameter = "props: { start: number }"
			}
			broken := "import { useState } from 'react';\n/** @param {{ start: number }} props */\nexport function Counter(" + parameter + ") {\n  const [count, setCount] = useState(props.start);\n  return <button onClick={() => setCount(count + 1)}>{count}</button>;\n}\nexport const view = <Counter start=\"bad\" />;\n"
			fixed := strings.Replace(broken, `start="bad"`, `start={1}`, 1)
			manager, ctx, root, clock := frameworkProject(t, map[string]string{
				"App" + extension: broken,
				"tsconfig.json":   `{"compilerOptions":{"strict":true,"jsx":"react-jsx","target":"ES2022","module":"ESNext","moduleResolution":"Bundler","allowJs":true,"checkJs":true,"noEmit":true,"skipLibCheck":true}}`,
			})
			file := filepath.Join(root, "App"+extension)
			uri := PathToURI(file)
			client, err := manager.clientForFile(ctx, file)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.ensureOpen(client, file, uri); err != nil {
				t.Fatal(err)
			}
			diagnostics, err := client.Diagnostics(ctx, uri)
			found := false
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "not assignable") {
					found = true
				}
			}
			if err != nil || !found {
				t.Fatalf("React Props type error missing: %v, %v", diagnostics, err)
			}
			lines := strings.Split(broken, "\n")
			hover, err := client.Hover(ctx, uri, Position{Line: 3, Character: strings.Index(lines[3], "count")})
			hoverJSON, _ := json.Marshal(hover)
			if err != nil || hover == nil || !strings.Contains(string(hoverJSON), "number") {
				t.Fatalf("React useState hover = %v, %v", hover, err)
			}
			locations, err := client.GotoDefinition(ctx, uri, Position{Line: 6, Character: strings.Index(lines[6], "Counter")})
			if err != nil || len(locations) == 0 || locations[0].Range.Start.Line != 2 {
				t.Fatalf("React component definition = %v, %v", locations, err)
			}
			rename, err := client.Rename(ctx, uri, Position{Line: 6, Character: strings.Index(lines[6], "Counter")}, "RenamedCounter")
			if err != nil || rename == nil {
				t.Fatalf("React rename: %v", err)
			}
			edits := 0
			for _, changes := range rename.Changes {
				edits += len(changes)
			}
			if edits < 2 {
				t.Fatalf("React rename did not include declaration and JSX use: %v", rename)
			}
			if err := os.WriteFile(file, []byte(fixed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := manager.NotifyFileChanged(file); err != nil {
				t.Fatal(err)
			}
			for _, cause := range []string{"edit", "idle", "crash"} {
				previous := client
				if cause == "idle" {
					clock.Advance(10 * time.Minute)
					manager.reapIdle()
				}
				if cause == "crash" {
					if err := client.process.Kill(); err != nil {
						t.Fatal(err)
					}
					<-client.processDone
				}
				client, err = manager.clientForFile(ctx, file)
				if err != nil {
					t.Fatal(err)
				}
				if cause != "edit" && previous == client {
					t.Fatalf("React %s reused a stale process", cause)
				}
				if err := manager.ensureOpen(client, file, uri); err != nil {
					t.Fatal(err)
				}
				diagnostics, err := client.Diagnostics(ctx, uri)
				for _, diagnostic := range diagnostics {
					if diagnostic.Severity == 1 {
						t.Errorf("React %s: %s", cause, diagnostic.Message)
					}
				}
				if err != nil {
					t.Fatalf("React %s diagnostics: %v", cause, err)
				}
			}
		})
	}
}
