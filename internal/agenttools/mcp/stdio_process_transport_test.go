package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

func TestStdioProcessTransportPreservesMCPProtocol(t *testing.T) {
	cfg := ServerConfig{
		Name:      "process-test",
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestMCPProcessHelperProcess", "--"},
		Env:       map[string]string{"SOLOQUEUE_MCP_HELPER": "1"},
		Transport: "stdio",
		Enabled:   true,
	}
	client := NewClientWithExecutor(cfg, tools.NewExecutor(), t.TempDir(), nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()

	if !client.IsConnected() {
		t.Fatal("client did not connect through stdio process transport")
	}
	list := client.ListTools()
	if len(list) != 1 || list[0].Name != "echo" {
		t.Fatalf("tools = %#v", list)
	}
	result, err := client.CallTool(ctx, "echo", map[string]any{"value": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("result = %q", result)
	}
}

func TestMCPProcessHelperProcess(t *testing.T) {
	if os.Getenv("SOLOQUEUE_MCP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil || len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "process-test", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{
					"name": "echo", "description": "echo",
					"inputSchema": map[string]any{"type": "object"},
				},
			}}
		case "tools/call":
			result = map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "ok"}},
			}
		default:
			result = map[string]any{}
		}
		if err := encoder.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(request.ID),
			"result":  result,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	os.Exit(0)
}
