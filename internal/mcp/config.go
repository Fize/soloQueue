package mcp

import (
	"encoding/json"
	"fmt"
)

// ServerConfig defines one MCP server in mcp.json.
type ServerConfig struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"` // "stdio" initially; "sse" later
	Enabled   bool              `json:"enabled"`
}

// Config is the top-level mcp.json structure.
// Custom JSON marshal/unmarshal supports both the standard mcpServers map format
// (Claude Desktop convention) and the legacy servers array format.
type Config struct {
	Servers []ServerConfig `json:"-"`
}

// serverConfigFromMap is used when decoding the mcpServers map format,
// where "enabled" is optional and defaults to true.
type serverConfigFromMap struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env,omitempty"`
	Transport string            `json:"transport"`
	Enabled   *bool             `json:"enabled,omitempty"`
}

func (c *Config) UnmarshalJSON(data []byte) error {
	// Try standard mcpServers map format first.
	var mcpWrapper struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &mcpWrapper); err == nil && len(mcpWrapper.MCPServers) > 0 {
		servers := make([]ServerConfig, 0, len(mcpWrapper.MCPServers))
		for name, raw := range mcpWrapper.MCPServers {
			var wire serverConfigFromMap
			if err := json.Unmarshal(raw, &wire); err != nil {
				return fmt.Errorf("mcpServers.%s: %w", name, err)
			}
			enabled := true
			if wire.Enabled != nil {
				enabled = *wire.Enabled
			}
			transport := wire.Transport
			if transport == "" {
				transport = "stdio"
			}
			servers = append(servers, ServerConfig{
				Name:      name,
				Command:   wire.Command,
				Args:      wire.Args,
				Env:       wire.Env,
				Transport: transport,
				Enabled:   enabled,
			})
		}
		c.Servers = servers
		return nil
	}

	// Fall back to legacy servers array format.
	var serversWrapper struct {
		Servers []ServerConfig `json:"servers"`
	}
	if err := json.Unmarshal(data, &serversWrapper); err != nil {
		return err
	}
	if serversWrapper.Servers == nil {
		serversWrapper.Servers = []ServerConfig{}
	}
	c.Servers = serversWrapper.Servers
	return nil
}

func (c Config) MarshalJSON() ([]byte, error) {
	mcpServers := make(map[string]serverConfigFromMap, len(c.Servers))
	for _, s := range c.Servers {
		enabled := s.Enabled
		mcpServers[s.Name] = serverConfigFromMap{
			Command:   s.Command,
			Args:      s.Args,
			Env:       s.Env,
			Transport: s.Transport,
			Enabled:   &enabled,
		}
	}
	return json.Marshal(map[string]any{"mcpServers": mcpServers})
}

// DefaultConfig returns an empty config with a non-nil Servers slice.
func DefaultConfig() Config {
	return Config{
		Servers: []ServerConfig{},
	}
}
