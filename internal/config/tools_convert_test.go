package config

import "testing"

// These tests drive the config → tools.Config Tavily API key plumbing.
// They are RED by design: ToolsConfig has no TavilyAPIKey/TavilyAPIKeyEnv
// fields yet, so they must fail to compile until the schema is extended.

func TestToToolsConfig_TavilyAPIKey(t *testing.T) {
	tc := ToolsConfig{TavilyAPIKey: "tvly-direct"}
	cfg := tc.ToToolsConfig()
	if cfg.TavilyAPIKey != "tvly-direct" {
		t.Errorf("cfg.TavilyAPIKey = %q, want %q", cfg.TavilyAPIKey, "tvly-direct")
	}
}

func TestToToolsConfig_TavilyAPIKeyEnvFallback(t *testing.T) {
	tc := ToolsConfig{TavilyAPIKeyEnv: "SOLOQUEUE_TAVILY_TEST_KEY"}
	t.Setenv("SOLOQUEUE_TAVILY_TEST_KEY", "tvly-env")
	cfg := tc.ToToolsConfig()
	if cfg.TavilyAPIKey != "tvly-env" {
		t.Errorf("cfg.TavilyAPIKey = %q, want %q", cfg.TavilyAPIKey, "tvly-env")
	}
}

func TestToToolsConfig_TavilyAPIKeyDirectWins(t *testing.T) {
	tc := ToolsConfig{TavilyAPIKeyEnv: "SOLOQUEUE_TAVILY_TEST_KEY", TavilyAPIKey: "tvly-direct"}
	t.Setenv("SOLOQUEUE_TAVILY_TEST_KEY", "tvly-env")
	cfg := tc.ToToolsConfig()
	if cfg.TavilyAPIKey != "tvly-direct" {
		t.Errorf("cfg.TavilyAPIKey = %q, want %q", cfg.TavilyAPIKey, "tvly-direct")
	}
}
