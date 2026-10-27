package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Requires a Windows workspace with typescript and typescript-language-server installed.
func TestWindowsRealTypescriptServer(t *testing.T) {
	root := os.Getenv("SOLOQUEUE_LSP_INTEGRATION_DIR")
	if root == "" {
		t.Skip("requires the Windows TypeScript integration workspace")
	}
	file := filepath.Join(root, "main.ts")
	if err := os.WriteFile(file, []byte("export let message = \"hello\";\nmessage.toUpperCase();\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := logger.New(root, logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	manager := NewManager(root, log)
	t.Cleanup(manager.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.Start(ctx, BuiltinServers()); err != nil {
		t.Fatal(err)
	}
	if len(manager.RunningServerIDs()) != 0 {
		t.Fatal("language server started before first use")
	}
	args, err := json.Marshal(map[string]any{"file": file, "line": 1, "character": 13})
	if err != nil {
		t.Fatal(err)
	}
	hover := &LSPTool{manager: manager, action: "hover", log: log}
	firstCtx, cancelFirst := context.WithCancel(ctx)
	result, err := hover.Execute(firstCtx, string(args))
	cancelFirst()
	if err != nil || !strings.Contains(result, "message: string") {
		t.Fatalf("first hover = %q, %v", result, err)
	}
	client, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	result, err = hover.Execute(ctx, string(args))
	if err != nil || !strings.Contains(result, "message: string") {
		t.Fatalf("reused hover = %q, %v", result, err)
	}
	reused, err := manager.clientForFile(ctx, file)
	if err != nil || reused != client {
		t.Fatalf("server was not reused: %v", err)
	}
	manager.Shutdown()
	select {
	case <-client.processDone:
	default:
		t.Fatal("server process was not stopped")
	}
}
