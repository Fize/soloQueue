package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Client manages a single MCP server connection via stdio transport.
type Client struct {
	cfg    ServerConfig
	client *mcpclient.Client
	tools  []mcp.Tool
	mu     sync.Mutex
	log    *logger.Logger
}

// NewClient creates a new Client for the given server config.
func NewClient(cfg ServerConfig, log *logger.Logger) *Client {
	return &Client{cfg: cfg, log: log}
}

// Connect starts the MCP server subprocess, initializes the session,
// and enumerates available tools.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil // already connected
	}

	env := os.Environ()
	for k, v := range c.cfg.Env {
		env = append(env, k+"="+v)
	}

	stdioTransport := transport.NewStdioWithOptions(
		c.cfg.Command,
		env,
		c.cfg.Args,
	)

	if err := stdioTransport.Start(ctx); err != nil {
		return fmt.Errorf("start stdio transport for %q: %w", c.cfg.Name, err)
	}

	client := mcpclient.NewClient(stdioTransport)

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "soloqueue",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		client.Close()
		return fmt.Errorf("initialize %q: %w", c.cfg.Name, err)
	}

	toolsResp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		client.Close()
		return fmt.Errorf("list tools for %q: %w", c.cfg.Name, err)
	}

	c.client = client
	c.tools = toolsResp.Tools

	if c.log != nil {
		c.log.Info(logger.CatMCP, "MCP server connected",
			"server", c.cfg.Name,
			"command", c.cfg.Command,
			"tools", len(c.tools),
		)
	}

	return nil
}

// Disconnect closes the MCP client and terminates the subprocess.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	err := c.client.Close()
	c.client = nil
	c.tools = nil

	if c.log != nil {
		c.log.Info(logger.CatMCP, "MCP server disconnected", "server", c.cfg.Name)
	}

	return err
}

// ListTools returns cached tool definitions.
func (c *Client) ListTools() []mcp.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return "", fmt.Errorf("MCP server %q not connected", c.cfg.Name)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := client.CallTool(ctx, req)
	if err != nil {
		// Attempt reconnect once on failure
		if c.log != nil {
			c.log.Warn(logger.CatMCP, "MCP tool call failed, attempting reconnect",
				"server", c.cfg.Name, "tool", name, "err", err.Error(),
			)
		}
		if reconnectErr := c.reconnect(ctx); reconnectErr != nil {
			return "", fmt.Errorf("call tool %q on %q: %w (reconnect also failed: %v)", name, c.cfg.Name, err, reconnectErr)
		}

		c.mu.Lock()
		client = c.client
		c.mu.Unlock()
		if client == nil {
			return "", fmt.Errorf("call tool %q on %q: server not reconnected", name, c.cfg.Name)
		}

		result, err = client.CallTool(ctx, req)
		if err != nil {
			return "", fmt.Errorf("call tool %q on %q after reconnect: %w", name, c.cfg.Name, err)
		}
	}

	content, err := extractTextContent(result)
	if err != nil {
		return "", fmt.Errorf("extract content from %q on %q: %w", name, c.cfg.Name, err)
	}

	return content, nil
}

func (c *Client) reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
		c.tools = nil
	}

	env := os.Environ()
	for k, v := range c.cfg.Env {
		env = append(env, k+"="+v)
	}

	stdioTransport := transport.NewStdioWithOptions(
		c.cfg.Command,
		env,
		c.cfg.Args,
	)

	if err := stdioTransport.Start(ctx); err != nil {
		return fmt.Errorf("restart stdio: %w", err)
	}

	client := mcpclient.NewClient(stdioTransport)

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "soloqueue",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		client.Close()
		return fmt.Errorf("reinitialize: %w", err)
	}

	toolsResp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		client.Close()
		return fmt.Errorf("relist tools: %w", err)
	}

	c.client = client
	c.tools = toolsResp.Tools

	if c.log != nil {
		c.log.Info(logger.CatMCP, "MCP server reconnected",
			"server", c.cfg.Name,
			"tools", len(c.tools),
		)
	}

	return nil
}

// IsConnected returns whether the client is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client != nil
}

func extractTextContent(result *mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", nil
	}

	var texts []string
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			texts = append(texts, tc.Text)
		} else {
			// For non-text content, include a JSON representation.
			data, err := json.Marshal(content)
			if err == nil {
				texts = append(texts, string(data))
			}
		}
	}

	return strings.Join(texts, "\n"), nil
}
