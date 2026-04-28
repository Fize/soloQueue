package tools

import (
	"testing"
)

// ─── Build ─────────────────────────────────────────────────────────────

func TestBuild_WithoutTavily(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	list := Build(cfg)
	if len(list) != 9 {
		t.Errorf("Build without TavilyAPIKey returned %d tools, want 9", len(list))
	}
	for _, tool := range list {
		if tool.Name() == "web_search" {
			t.Errorf("web_search should be omitted when TavilyAPIKey empty")
		}
	}
}

func TestBuild_WithTavily(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	list := Build(cfg)
	if len(list) != 10 {
		t.Errorf("Build with TavilyAPIKey returned %d tools, want 10", len(list))
	}
	hasWebSearch := false
	for _, tool := range list {
		if tool.Name() == "web_search" {
			hasWebSearch = true
		}
	}
	if !hasWebSearch {
		t.Errorf("web_search should be included when TavilyAPIKey set")
	}
}

func TestBuild_ReturnsUniqueToolNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	seen := map[string]bool{}
	for _, tool := range Build(cfg) {
		if seen[tool.Name()] {
			t.Errorf("duplicate tool name %q", tool.Name())
		}
		seen[tool.Name()] = true
	}
}

// TestBuild_AllToolsHaveNonEmptyDescription sanity-checks that every built tool
// carries a description string (LLM reads this to pick the right tool).
func TestBuild_AllToolsHaveNonEmptyDescription(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = "tvly-test"
	for _, tool := range Build(cfg) {
		if tool.Description() == "" {
			t.Errorf("tool %q has empty Description", tool.Name())
		}
	}
}

// ─── DefaultConfig ─────────────────────────────────────────────────────

func TestDefaultConfig_SaneValues(t *testing.T) {
	c := DefaultConfig()
	if c.MaxFileSize <= 0 || c.MaxWriteSize <= 0 {
		t.Errorf("MaxFileSize=%d MaxWriteSize=%d should be positive", c.MaxFileSize, c.MaxWriteSize)
	}
	if !c.HTTPBlockPrivate {
		t.Error("HTTPBlockPrivate default should be true")
	}
	if c.TavilyEndpoint == "" {
		t.Error("TavilyEndpoint should have default")
	}
}
