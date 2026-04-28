package config

import (
	"os"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ToToolsConfig 将 config.ToolsConfig 转换为 tools.Config。
// allowedDirs 会与配置中的 AllowedDirs 合并作为沙箱白名单。
func (tc ToolsConfig) ToToolsConfig(allowedDirs []string) tools.Config {
	tavilyKey := ""
	if tc.TavilyAPIKeyEnv != "" {
		tavilyKey = os.Getenv(tc.TavilyAPIKeyEnv)
	}
	return tools.Config{
		AllowedDirs:        allowedDirs,
		MaxFileSize:        defaultInt64(tc.MaxFileSize, 1<<20),
		MaxMatches:         DefaultInt(tc.MaxMatches, 100),
		MaxLineLen:         DefaultInt(tc.MaxLineLen, 500),
		MaxGlobItems:       DefaultInt(tc.MaxGlobItems, 1000),
		MaxWriteSize:       defaultInt64(tc.MaxWriteSize, 1<<20),
		MaxMultiWriteBytes: defaultInt64(tc.MaxMultiWriteBytes, 10<<20),
		MaxMultiWriteFiles: DefaultInt(tc.MaxMultiWriteFiles, 50),
		MaxReplaceEdits:    DefaultInt(tc.MaxReplaceEdits, 50),

		HTTPAllowedHosts: tc.HTTPAllowedHosts,
		HTTPMaxBody:      defaultInt64(tc.HTTPMaxBody, 5<<20),
		HTTPTimeout:      msToDuration(tc.HTTPTimeoutMs, 10*time.Second),
		HTTPBlockPrivate: tc.HTTPBlockPrivate,

		ShellBlockRegexes:   tc.ShellBlockRegexes,
		ShellConfirmRegexes: tc.ShellConfirmRegexes,
		ShellTimeout:        msToDuration(tc.ShellTimeoutMs, 30*time.Second),
		ShellMaxOutput:      defaultInt64(tc.ShellMaxOutput, 256<<10),

		TavilyAPIKey:   tavilyKey,
		TavilyEndpoint: defaultString(tc.TavilyEndpoint, "https://api.tavily.com/search"),
		TavilyTimeout:  msToDuration(tc.TavilyTimeoutMs, 15*time.Second),
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

// DefaultInt returns def if v <= 0, otherwise returns v.
func DefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func defaultInt64(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func msToDuration(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}
