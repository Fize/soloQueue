//go:build integration

package lsp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Requires typescript-language-server on PATH and the Web Console's installed
// TypeScript dependency. Each case uses an isolated project and real subprocess.
func TestTypescriptLazyLoadingIntegration(t *testing.T) {
	if _, err := exec.LookPath("typescript-language-server"); err != nil {
		t.Fatal("integration test requires typescript-language-server on PATH")
	}
	_, source, _, _ := runtime.Caller(0)
	typescript := filepath.Join(filepath.Dir(source), "../../../../web/node_modules/typescript")
	if _, err := os.Stat(filepath.Join(typescript, "lib", "tsserver.js")); err != nil {
		t.Fatalf("install Web Console dependencies before integration testing: %v", err)
	}
	for _, ext := range []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(typescript, filepath.Join(root, "node_modules", "typescript")); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "main"+ext)
			for path, content := range map[string]string{
				filepath.Join(root, "tsconfig.json"): `{"compilerOptions":{"allowJs":true,"checkJs":true,"noEmit":true,"jsx":"preserve"}}`,
				file:                                 "export let message = \"hello\";\nexport const upper = message.toUpperCase();\nmessage = 42;\n",
			} {
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
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
			ctx, cancel := context.WithTimeout(iface.ContextWithWorkDir(context.Background(), root), 20*time.Second)
			defer cancel()
			if err := manager.Start(ctx, BuiltinServers()); err != nil {
				t.Fatal(err)
			}
			if len(manager.RunningServerIDs()) != 0 {
				t.Fatal("language server started before first use")
			}
			args, _ := json.Marshal(map[string]any{"file": file, "line": 1, "character": 13})
			hover := &LSPTool{manager: manager, action: "hover", log: log}
			firstCall, cancelFirstCall := context.WithCancel(ctx)
			result, err := hover.Execute(firstCall, string(args))
			cancelFirstCall()
			if err != nil || !strings.Contains(result, "message: string") {
				t.Fatalf("hover after lazy start = %s, %v", result, err)
			}
			if ids := manager.RunningServerIDs(); len(ids) != 1 || !strings.HasPrefix(ids[0], "typescript@") {
				t.Fatalf("running servers = %v, want one TypeScript server", ids)
			}
			client, err := manager.clientForFile(ctx, file)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-client.done:
				t.Fatal("language server died when its first tool call ended")
			case <-time.After(100 * time.Millisecond):
			}
			locations, err := client.GotoDefinition(ctx, PathToURI(file), Position{Line: 1, Character: 22})
			if err != nil || len(locations) != 1 || locations[0].Range.Start.Line != 0 {
				t.Fatalf("definition = %v, %v", locations, err)
			}
			diagnostics, err := client.Diagnostics(ctx, PathToURI(file))
			found := false
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, "not assignable") {
					found = true
				}
			}
			if err != nil || !found {
				t.Fatalf("diagnostics lost type error: %v, %v", diagnostics, err)
			}
			if err := os.WriteFile(file, []byte("export let message = \"hello\";\nexport const upper = message.toUpperCase();\nmessage = \"fixed\";\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := manager.NotifyFileChanged(file); err != nil {
				t.Fatal(err)
			}
			diagnostics, err = client.Diagnostics(ctx, PathToURI(file))
			if err != nil || len(diagnostics) != 0 {
				t.Fatalf("diagnostics did not clear after edit: %v, %v", diagnostics, err)
			}
			reused, err := manager.clientForFile(ctx, file)
			if err != nil || reused != client {
				t.Fatal("subsequent use did not reuse the running server")
			}
			for _, cause := range []string{"idle", "crash"} {
				if cause == "idle" {
					clock.Advance(10 * time.Minute)
					manager.reapIdle()
				} else {
					if err := client.process.Kill(); err != nil {
						t.Fatal(err)
					}
					<-client.processDone
				}
				result, err := hover.Execute(ctx, string(args))
				if err != nil || !strings.Contains(result, "message: string") {
					t.Fatalf("%s recovery hover = %s, %v", cause, result, err)
				}
				fresh, err := manager.clientForFile(ctx, file)
				if err != nil || fresh == client {
					t.Fatalf("%s recovery did not start a new process", cause)
				}
				diagnostics, err := fresh.Diagnostics(ctx, PathToURI(file))
				if err != nil || len(diagnostics) != 0 {
					t.Fatalf("%s recovery diagnostics = %v, %v", cause, diagnostics, err)
				}
				client = fresh
			}
		})
	}
}
