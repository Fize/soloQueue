package mcp

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
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

// DefaultConfig returns an empty config with a non-nil Servers slice.
func DefaultConfig() Config {
	return Config{
		Servers: []ServerConfig{},
	}
}
