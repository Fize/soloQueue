package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ToolsPatch distinguishes omitted fields from explicit zero values.
type ToolsPatch struct {
	MaxFileSize        *int64              `json:"maxFileSize"`
	MaxMatches         *int                `json:"maxMatches"`
	MaxLineLen         *int                `json:"maxLineLen"`
	MaxGlobItems       *int                `json:"maxGlobItems"`
	MaxWriteSize       *int64              `json:"maxWriteSize"`
	MaxMultiWriteBytes *int64              `json:"maxMultiWriteBytes"`
	MaxMultiWriteFiles *int                `json:"maxMultiWriteFiles"`
	MaxReplaceEdits    *int                `json:"maxReplaceEdits"`
	HTTPAllowedHosts   *[]string           `json:"httpAllowedHosts,omitempty"`
	HTTPMaxBody        *int64              `json:"httpMaxBody"`
	HTTPTimeoutMs      *int                `json:"httpTimeoutMs"`
	HTTPBlockPrivate   *bool               `json:"httpBlockPrivate"`
	ShellBlockRegexes  *[]string           `json:"shellBlockRegexes"`
	ShellMaxOutput     *int64              `json:"shellMaxOutput"`
	WebSearchTimeoutMs *int                `json:"webSearchTimeoutMs"`
	TavilyAPIKey       *string             `json:"tavilyApiKey"`
	TavilyAPIKeyEnv    *string             `json:"tavilyApiKeyEnv"`
	ImageModels        *[]ImageModelConfig `json:"imageModels,omitempty"`
}

// PatchTools merges only supplied fields while holding the configuration lock.
func (s *GlobalService) PatchTools(p ToolsPatch) (ToolsConfig, error) {
	if p.ImageModels != nil {
		if err := ValidateImageModels(*p.ImageModels); err != nil {
			return ToolsConfig{}, err
		}
	}
	st, err := s.Set(func(st *Settings) {
		if p.MaxFileSize != nil {
			st.Tools.MaxFileSize = *p.MaxFileSize
		}
		if p.MaxMatches != nil {
			st.Tools.MaxMatches = *p.MaxMatches
		}
		if p.MaxLineLen != nil {
			st.Tools.MaxLineLen = *p.MaxLineLen
		}
		if p.MaxGlobItems != nil {
			st.Tools.MaxGlobItems = *p.MaxGlobItems
		}
		if p.MaxWriteSize != nil {
			st.Tools.MaxWriteSize = *p.MaxWriteSize
		}
		if p.MaxMultiWriteBytes != nil {
			st.Tools.MaxMultiWriteBytes = *p.MaxMultiWriteBytes
		}
		if p.MaxMultiWriteFiles != nil {
			st.Tools.MaxMultiWriteFiles = *p.MaxMultiWriteFiles
		}
		if p.MaxReplaceEdits != nil {
			st.Tools.MaxReplaceEdits = *p.MaxReplaceEdits
		}
		if p.HTTPAllowedHosts != nil {
			st.Tools.HTTPAllowedHosts = *p.HTTPAllowedHosts
		}
		if p.HTTPMaxBody != nil {
			st.Tools.HTTPMaxBody = *p.HTTPMaxBody
		}
		if p.HTTPTimeoutMs != nil {
			st.Tools.HTTPTimeoutMs = *p.HTTPTimeoutMs
		}
		if p.HTTPBlockPrivate != nil {
			st.Tools.HTTPBlockPrivate = *p.HTTPBlockPrivate
		}
		if p.ShellBlockRegexes != nil {
			st.Tools.ShellBlockRegexes = *p.ShellBlockRegexes
		}
		if p.ShellMaxOutput != nil {
			st.Tools.ShellMaxOutput = *p.ShellMaxOutput
		}
		if p.WebSearchTimeoutMs != nil {
			st.Tools.WebSearchTimeoutMs = *p.WebSearchTimeoutMs
		}
		if p.TavilyAPIKey != nil {
			st.Tools.TavilyAPIKey = *p.TavilyAPIKey
		}
		if p.TavilyAPIKeyEnv != nil {
			st.Tools.TavilyAPIKeyEnv = *p.TavilyAPIKeyEnv
		}
		if p.ImageModels != nil {
			st.Tools.ImageModels = *p.ImageModels
		}
	})
	return st.Tools, err
}

// ValidateImageModels validates editable image configuration without contacting providers.
func ValidateImageModels(models []ImageModelConfig) error {
	ids := map[string]bool{}
	enabled, defaults := 0, 0
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("image model ID and name are required")
		}
		if ids[m.ID] {
			return fmt.Errorf("image model IDs must be unique")
		}
		ids[m.ID] = true
		if m.Provider != "tencent" {
			return fmt.Errorf("only tencent image generation is supported")
		}
		if m.APIBaseHost != "" {
			u, err := url.Parse("https://" + m.APIBaseHost)
			if err != nil || u.Hostname() == "" || u.Host != m.APIBaseHost || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("image API host must be a hostname without a scheme or path")
			}
		}
		if m.IsDefault && !m.Enabled {
			return fmt.Errorf("default image model must be enabled")
		}
		if m.Enabled {
			if strings.TrimSpace(m.APIBaseHost) == "" || strings.TrimSpace(m.Region) == "" {
				return fmt.Errorf("enabled image models require API host and region")
			}
			enabled++
			if (strings.TrimSpace(m.SecretId) == "" && strings.TrimSpace(m.SecretIdEnv) == "") || (strings.TrimSpace(m.SecretKey) == "" && strings.TrimSpace(m.SecretKeyEnv) == "") {
				return fmt.Errorf("enabled image models require SecretId and SecretKey values or environment variable names")
			}
			if m.IsDefault {
				defaults++
			}
		}
	}
	if enabled > 0 && defaults != 1 {
		return fmt.Errorf("enabled image models require exactly one enabled default")
	}
	return nil
}
