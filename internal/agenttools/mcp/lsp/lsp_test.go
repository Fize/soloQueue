package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

type stopTestProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *stopTestProcess) Stdin() io.WriteCloser { return nil }
func (p *stopTestProcess) Stdout() io.Reader     { return strings.NewReader("") }
func (p *stopTestProcess) Stderr() io.Reader     { return strings.NewReader("") }
func (p *stopTestProcess) Wait() error {
	<-p.done
	return nil
}
func (p *stopTestProcess) Kill() error {
	p.once.Do(func() { close(p.done) })
	return nil
}

func TestClientStopKillsUninitializedProcessBeforeWaiting(t *testing.T) {
	process := &stopTestProcess{done: make(chan struct{})}
	client := &Client{
		process:  process,
		shutdown: make(chan struct{}),
		done:     process.done,
	}

	start := time.Now()
	client.Stop()
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("Stop blocked for %v; uninitialized process must be killed before waiting", elapsed)
	}
	select {
	case <-process.done:
	default:
		t.Fatal("Stop returned without killing the uninitialized process")
	}
}

func TestClientStartInitializesWithoutHoldingClientMutex(t *testing.T) {
	workDir := t.TempDir()
	client := NewClientWithExecutor(
		"test-lsp",
		"go",
		PathToURI(workDir),
		os.Args[0],
		[]string{"-test.run=TestLSPHelperProcess", "--"},
		workDir,
		tools.NewExecutor(),
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- client.Start(ctx) }()

	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("Client.Start returned error: %v", err)
		}
		client.Stop()
	case <-time.After(3 * time.Second):
		if client.process != nil {
			_ = client.process.Kill()
		}
		t.Fatal("Client.Start blocked while initializing the LSP server")
	}
}

func TestLSPHelperProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "--" {
			continue
		}
		reader := bufio.NewReader(os.Stdin)
		for {
			body, err := readLSPHelperMessage(reader)
			if err != nil {
				return
			}
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.Unmarshal(body, &request); err != nil || len(request.ID) == 0 {
				if request.Method == "exit" {
					return
				}
				continue
			}

			result := map[string]any{}
			if request.Method == "initialize" {
				result = map[string]any{
					"capabilities": map[string]any{},
					"serverInfo":   map[string]any{"name": "test-lsp", "version": "1.0.0"},
				}
			}
			response, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  result,
			})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(response), response)
		}
	}
}

func readLSPHelperMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")), "%d", &contentLength); err != nil {
				return nil, err
			}
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("invalid content length %d", contentLength)
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func TestBuiltinServers_NotEmpty(t *testing.T) {
	servers := BuiltinServers()
	if len(servers) == 0 {
		t.Error("BuiltinServers returned empty list")
	}
}

func TestBuiltinServers_HaveRequiredFields(t *testing.T) {
	for _, s := range BuiltinServers() {
		if s.ID == "" {
			t.Error("server has empty ID")
		}
		if s.Command == "" {
			t.Errorf("server %s has empty Command", s.ID)
		}
		if len(s.Extensions) == 0 {
			t.Errorf("server %s has no Extensions", s.ID)
		}
		if len(s.Languages) == 0 {
			t.Errorf("server %s has no Languages", s.ID)
		}
	}
}

func TestBuiltinServers_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, s := range BuiltinServers() {
		if seen[s.ID] {
			t.Errorf("duplicate server ID: %s", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestResolveCommand_WithResolve(t *testing.T) {
	def := ServerDef{
		Resolve: func(workspacePath string) string {
			return "/custom/path/to/server"
		},
	}
	result := resolveCommand(def, "/workspace")
	if result != "/custom/path/to/server" {
		t.Errorf("resolveCommand = %q, want /custom/path/to/server", result)
	}
}

func TestResolveCommand_WithResolveReturnsEmpty(t *testing.T) {
	def := ServerDef{
		Resolve: func(workspacePath string) string {
			return ""
		},
	}
	result := resolveCommand(def, "/workspace")
	if result != "" {
		t.Errorf("resolveCommand = %q, want empty", result)
	}
}

func TestResolveCommand_LookPathFallback(t *testing.T) {
	def := ServerDef{
		Command: "nonexistent-binary-xyz",
	}
	result := resolveCommand(def, "/workspace")
	if result != "" {
		t.Errorf("resolveCommand for nonexistent binary = %q, want empty", result)
	}
}

func TestServerDef_Fields(t *testing.T) {
	def := ServerDef{
		ID:          "test-lsp",
		Command:     "test-server",
		Args:        []string{"--stdio"},
		Languages:   []string{"go"},
		Extensions:  []string{".go"},
		InstallHint: "go install example.com/test@latest",
	}
	if def.ID != "test-lsp" {
		t.Errorf("ID = %q", def.ID)
	}
	if def.Command != "test-server" {
		t.Errorf("Command = %q", def.Command)
	}
	if len(def.Args) != 1 || def.Args[0] != "--stdio" {
		t.Errorf("Args = %v", def.Args)
	}
	if len(def.Languages) != 1 || def.Languages[0] != "go" {
		t.Errorf("Languages = %v", def.Languages)
	}
}
