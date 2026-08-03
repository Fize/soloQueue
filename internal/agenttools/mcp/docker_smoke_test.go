package mcp

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

func TestSandboxMCPProtocolSmoke(t *testing.T) {
	if os.Getenv("SOLOQUEUE_DOCKER_SMOKE") != "1" {
		t.Skip("set SOLOQUEUE_DOCKER_SMOKE=1 to run the real backend test")
	}
	workspace := t.TempDir()
	serverPath := filepath.Join(workspace, "mcp-server.js")
	serverSource := `
const fs = require("fs");
fs.appendFileSync(__dirname + "/requests.log", "startup:" + process.pid + "\n");
function handleLine(line) {
  fs.appendFileSync(__dirname + "/requests.log", "request:" + line + "\n");
  let request;
  try { request = JSON.parse(line); } catch (_) { return; }
  if (request.id === undefined) return;
  let result = {};
  if (request.method === "initialize") {
    result = {
      protocolVersion: "2025-03-26",
      capabilities: { tools: {} },
      serverInfo: { name: "sandbox-smoke", version: "1.0.0" }
    };
  } else if (request.method === "tools/list") {
    result = {
      tools: [{
        name: "where",
        description: "report sandbox identity",
        inputSchema: { type: "object" }
      }]
    };
  } else if (request.method === "tools/call") {
    result = {
      content: [{
        type: "text",
        text: "sandbox-ok:" + process.getuid() + ":" + process.env.HOME
      }]
    };
  }
  const response = JSON.stringify({
    jsonrpc: "2.0",
    id: request.id,
    result
  }) + "\n";
  fs.writeSync(1, response);
  fs.appendFileSync(__dirname + "/requests.log", "flushed:" + request.id + "\n");
  fs.appendFileSync(__dirname + "/requests.log", "response:" + request.id + "\n");
}
let buffer = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => {
  buffer += chunk;
  for (;;) {
    const newline = buffer.indexOf("\n");
    if (newline < 0) break;
    const line = buffer.slice(0, newline);
    buffer = buffer.slice(newline + 1);
    handleLine(line);
  }
});
process.stdin.resume();
`
	if err := os.WriteFile(serverPath, []byte(serverSource), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeManager := tools.NewRuntimeManager(tools.RuntimeHost, nil)
	defer runtimeManager.Close()
	runtime := runtimeManager.ViewForPolicy(
		tools.RuntimeSandbox,
		"mcp:smoke:sandbox",
		workspace,
		"",
		false,
	)
	check, err := runtime.RunCommand(context.Background(), "/usr/bin/node --check ./mcp-server.js", tools.RunCommandOptions{
		WorkingDirectory: workspace,
		MaxOutput:        16 << 10,
	})
	if err != nil || check.ExitCode != 0 {
		t.Fatalf("sandbox MCP helper syntax: exit=%d stderr=%s err=%v", check.ExitCode, check.Stderr, err)
	}
	probe, err := runtime.RunCommand(context.Background(), "/usr/bin/node ./mcp-server.js", tools.RunCommandOptions{
		WorkingDirectory: workspace,
		Stdin:            `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n",
		MaxOutput:        16 << 10,
	})
	if err != nil || probe.ExitCode != 0 || !strings.Contains(string(probe.Stdout), `"id":1`) {
		t.Fatalf("sandbox MCP helper protocol probe: exit=%d stdout=%s stderr=%s err=%v",
			probe.ExitCode, probe.Stdout, probe.Stderr, err)
	}
	manual, err := runtime.StartProcess(context.Background(), tools.ProcessSpec{
		Command:          "/usr/bin/node",
		Args:             []string{serverPath},
		WorkingDirectory: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manual.Stdin().Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"initialize"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	manualResponse := make(chan string, 1)
	manualError := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(manual.Stdout()).ReadString('\n')
		manualResponse <- line
	}()
	go func() {
		data, _ := io.ReadAll(manual.Stderr())
		manualError <- string(data)
	}()
	select {
	case line := <-manualResponse:
		if !strings.Contains(line, `"id":2`) {
			requests, _ := os.ReadFile(filepath.Join(workspace, "requests.log"))
			var stderr string
			select {
			case stderr = <-manualError:
			case <-time.After(time.Second):
			}
			t.Fatalf("manual MCP stream response = %q; server trace=%s stderr=%s", line, requests, stderr)
		}
	case <-time.After(2 * time.Second):
		requests, _ := os.ReadFile(filepath.Join(workspace, "requests.log"))
		_ = manual.Kill()
		var stderr string
		select {
		case stderr = <-manualError:
		case <-time.After(time.Second):
		}
		t.Fatalf("manual MCP stream response timed out; server trace=%s stderr=%s", requests, stderr)
	}
	if err := manual.Kill(); err != nil {
		t.Fatal(err)
	}
	manualDone := make(chan error, 1)
	go func() { manualDone <- manual.Wait() }()
	select {
	case <-manualDone:
	case <-time.After(time.Second):
		t.Fatal("manual sandbox MCP process did not finish after Kill")
	}
	client := NewClientWithRuntime(ServerConfig{
		Name:      "sandbox-smoke",
		Command:   "/usr/bin/node",
		Args:      []string{serverPath},
		Transport: "stdio",
		Enabled:   true,
	}, runtime, workspace, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		requests, _ := os.ReadFile(filepath.Join(workspace, "requests.log"))
		t.Fatalf("%v; server trace=%s", err, requests)
	}
	if tools := client.ListTools(); len(tools) != 1 || tools[0].Name != "where" {
		t.Fatalf("unexpected sandbox MCP catalog: %#v", tools)
	}
	result, err := client.CallTool(ctx, "where", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result, "sandbox-ok:") ||
		strings.Contains(result, "sandbox-ok:0:") ||
		!strings.HasSuffix(result, ":/tmp/soloqueue-home") {
		t.Fatalf("MCP did not execute under sandbox identity: %q", result)
	}
	if err := client.Disconnect(); err != nil {
		t.Fatalf("disconnect sandbox MCP: %v", err)
	}
}
