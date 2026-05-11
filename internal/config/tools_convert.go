package config

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ToToolsConfig 将 config.ToolsConfig 转换为 tools.Config。
func (tc ToolsConfig) ToToolsConfig() tools.Config {
	return tools.Config{
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
		HTTPTimeout:      msToDuration(tc.HTTPTimeoutMs, 10*time.Minute),
		HTTPBlockPrivate: tc.HTTPBlockPrivate,

		ShellBlockRegexes:   tc.ShellBlockRegexes,
		ShellConfirmRegexes: tc.ShellConfirmRegexes,
		ShellMaxOutput:      defaultInt64(tc.ShellMaxOutput, 256<<10),

		WebSearchTimeout: msToDuration(tc.WebSearchTimeoutMs, 10*time.Minute),

		ImageModels: toImgModelCfgs(tc.ImageModels),
	}
}

func toImgModelCfgs(cfgs []ImageModelConfig) []tools.ImgModelCfg {
	out := make([]tools.ImgModelCfg, len(cfgs))
	for i, c := range cfgs {
		out[i] = tools.ImgModelCfg{
			ID: c.ID, Name: c.Name, Provider: c.Provider,
			SecretIdEnv: c.SecretIdEnv, SecretKeyEnv: c.SecretKeyEnv,
			APIKeyEnv: c.APIKeyEnv, APIBaseHost: c.APIBaseHost,
			Region: c.Region, IsDefault: c.IsDefault, Enabled: c.Enabled,
		}
	}
	return out
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

func msToDuration(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}
