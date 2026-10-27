package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// StorageConfig separates disposable diagnostics from user-owned conversation history.
type StorageConfig struct {
	RuntimeLogs      RuntimeLogPolicy  `json:"runtime_logs" yaml:"runtime_logs"`
	SessionHistory   HistoryPolicy     `json:"session_history" yaml:"session_history"`
	ExecutionDetails DetailPolicy      `json:"execution_details" yaml:"execution_details"`
	CronHistory      CronHistoryPolicy `json:"cron_history" yaml:"cron_history"`
}
type FilePolicy struct {
	MaxFileMB     int `json:"max_file_mb" yaml:"max_file_mb"`
	RetentionDays int `json:"retention_days" yaml:"retention_days"`
	MaxFiles      int `json:"max_files" yaml:"max_files"`
	MaxTotalMB    int `json:"max_total_mb" yaml:"max_total_mb"`
}
type RuntimeLogPolicy struct {
	FilePolicy        `yaml:",inline"`
	HTTPOverride      bool `json:"http_override" yaml:"http_override"`
	HTTPMaxFileMB     int  `json:"http_max_file_mb" yaml:"http_max_file_mb"`
	HTTPRetentionDays int  `json:"http_retention_days" yaml:"http_retention_days"`
	HTTPMaxFiles      int  `json:"http_max_files" yaml:"http_max_files"`
}
type HistoryPolicy struct {
	FilePolicy `yaml:",inline"`
	WarningMB  int `json:"warning_mb" yaml:"warning_mb"`
}
type DetailPolicy struct {
	FilePolicy `yaml:",inline"`
	MaxTurnMB  int `json:"max_turn_mb" yaml:"max_turn_mb"`
}
type CronHistoryPolicy struct {
	FilePolicy `yaml:",inline"`
	MaxPerTask int `json:"max_per_task" yaml:"max_per_task"`
}

func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		RuntimeLogs:      RuntimeLogPolicy{FilePolicy: FilePolicy{50, 15, 30, 1024}, HTTPMaxFileMB: 50, HTTPRetentionDays: 15, HTTPMaxFiles: 30},
		SessionHistory:   HistoryPolicy{FilePolicy: FilePolicy{MaxFileMB: 50}, WarningMB: 1024},
		ExecutionDetails: DetailPolicy{FilePolicy: FilePolicy{MaxFileMB: 50, RetentionDays: 30, MaxTotalMB: 1024}, MaxTurnMB: 50},
		CronHistory:      CronHistoryPolicy{FilePolicy: FilePolicy{MaxFileMB: 50, RetentionDays: 30, MaxTotalMB: 512}},
	}
}
func (p StorageConfig) Validate() error {
	groups := []FilePolicy{p.RuntimeLogs.FilePolicy, p.SessionHistory.FilePolicy, p.ExecutionDetails.FilePolicy, p.CronHistory.FilePolicy}
	for _, g := range groups {
		if g.MaxFileMB < 1 || g.MaxFileMB > 1024 || g.RetentionDays < 0 || g.RetentionDays > 36500 || g.MaxFiles < 0 || g.MaxFiles > 100000 || g.MaxTotalMB < 0 || g.MaxTotalMB > 1048576 {
			return fmt.Errorf("storage limits out of range: file size 1–1024 MiB, retention 0–36500 days, files 0–100000, budget 0–1048576 MiB")
		}
	}
	if p.SessionHistory.MaxTotalMB != 0 || p.SessionHistory.MaxFiles != 0 {
		return fmt.Errorf("conversation history supports retention days and a warning threshold; automatic capacity/count deletion is not supported")
	}
	if p.SessionHistory.WarningMB < 0 || p.SessionHistory.WarningMB > 1048576 || p.ExecutionDetails.MaxTurnMB < 1 || p.ExecutionDetails.MaxTurnMB > 1024 || p.CronHistory.MaxPerTask < 0 || p.CronHistory.MaxPerTask > 100000 {
		return fmt.Errorf("invalid history warning, per-turn limit, or per-task count")
	}
	if p.RuntimeLogs.HTTPMaxFileMB < 1 || p.RuntimeLogs.HTTPMaxFileMB > 1024 || p.RuntimeLogs.HTTPRetentionDays < 0 || p.RuntimeLogs.HTTPRetentionDays > 36500 || p.RuntimeLogs.HTTPMaxFiles < 0 || p.RuntimeLogs.HTTPMaxFiles > 100000 {
		return fmt.Errorf("invalid HTTP log limits")
	}
	return nil
}

// EffectiveStorage retains the legacy file-size setting when no new override was supplied.
func (s Settings) EffectiveStorage() StorageConfig {
	p := s.Storage
	if p == (StorageConfig{}) {
		p = DefaultStorageConfig()
		if s.Session.TimelineMaxFileMB > 0 {
			p.SessionHistory.MaxFileMB = s.Session.TimelineMaxFileMB
		}
		return p
	}
	if p.RuntimeLogs.MaxFileMB == 0 {
		p.RuntimeLogs.MaxFileMB = 50
	}
	if p.SessionHistory.MaxFileMB == 0 {
		p.SessionHistory.MaxFileMB = 50
	}
	if p.ExecutionDetails.MaxFileMB == 0 {
		p.ExecutionDetails.MaxFileMB = 50
	}
	if p.CronHistory.MaxFileMB == 0 {
		p.CronHistory.MaxFileMB = 50
	}
	return p
}

// UnmarshalYAML gives the new setting precedence while accepting older settings files.
func (s *Settings) UnmarshalYAML(node *yaml.Node) error {
	type plain Settings
	value := plain(*s)
	if err := node.Decode(&value); err != nil {
		return err
	}
	legacy, newSize, storagePresent := false, false, false
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i].Value, node.Content[i+1]
		if key == "session" {
			for j := 0; j+1 < len(val.Content); j += 2 {
				if val.Content[j].Value == "timeline_max_file_mb" {
					legacy = true
				}
			}
		}
		if key == "storage" {
			storagePresent = true
			for j := 0; j+1 < len(val.Content); j += 2 {
				if val.Content[j].Value == "session_history" {
					v := val.Content[j+1]
					for k := 0; k+1 < len(v.Content); k += 2 {
						if v.Content[k].Value == "max_file_mb" {
							newSize = true
						}
					}
				}
			}
		}
	}
	*s = Settings(value)
	if legacy && !newSize && s.Session.TimelineMaxFileMB > 0 {
		s.Storage.SessionHistory.MaxFileMB = s.Session.TimelineMaxFileMB
	}
	if storagePresent {
		if err := s.EffectiveStorage().Validate(); err != nil {
			return err
		}
	}
	return nil
}
