package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// Tool adapts an MCP tool to the soloqueue tools.Tool interface.
// The tool name follows the convention mcp__<server>__<tool>.
type Tool struct {
	name        string
	server      string
	mcpToolName string
	description string
	params      json.RawMessage
	client      *Client
}

// NewMCPTool creates a new Tool adapter.
func NewMCPTool(server string, mcpTool mcp.Tool, client *Client) *Tool {
	name := "mcp__" + server + "__" + mcpTool.Name
	desc := "[MCP:" + server + "] " + mcpTool.Description
	if desc == "[MCP:"+server+"] " {
		desc = "[MCP:" + server + "] " + mcpTool.Name
	}

	var params json.RawMessage
	if mcpTool.RawInputSchema != nil {
		params = mcpTool.RawInputSchema
	} else {
		schema, err := json.Marshal(mcpTool.InputSchema)
		if err == nil {
			params = json.RawMessage(schema)
		}
	}

	return &Tool{
		name:        name,
		server:      server,
		mcpToolName: mcpTool.Name,
		description: desc,
		params:      params,
		client:      client,
	}
}

// Name returns the tool name in mcp__<server>__<tool> format.
func (t *Tool) Name() string { return t.name }

// Description returns the tool description prefixed with [MCP:<server>].
func (t *Tool) Description() string { return t.description }

// Parameters returns the JSON Schema for the tool's parameters.
func (t *Tool) Parameters() json.RawMessage { return t.params }

// Execute calls the MCP tool and returns the result.
func (t *Tool) Execute(ctx context.Context, args string) (string, error) {
	var argsMap map[string]any
	if args != "" {
		if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
			return "", fmt.Errorf("MCP tool %q: parse args: %w", t.name, err)
		}
	}

	result, err := t.client.CallTool(ctx, t.mcpToolName, argsMap)
	if err != nil {
		return "error: " + err.Error(), nil
	}

	return result, nil
}
