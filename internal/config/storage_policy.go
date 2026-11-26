package config

import "github.com/xiaobaitu/soloqueue/internal/storagepolicy"

// StoragePolicy is the single conversion from the configured storage limits to
// the value every writer and cleaner is judged against.
func (s StorageConfig) StoragePolicy() storagepolicy.Policy {
	return storagepolicy.Policy{
		RuntimeLogs: storagepolicy.RuntimeLogPolicy{
			FilePolicy: storagepolicy.FilePolicy{
				MaxFileBytes:  int64(s.RuntimeLogs.MaxFileMB) * storagepolicy.MiB,
				RetentionDays: s.RuntimeLogs.RetentionDays,
				MaxFiles:      s.RuntimeLogs.MaxFiles,
				MaxTotalBytes: int64(s.RuntimeLogs.MaxTotalMB) * storagepolicy.MiB,
			},
			HTTPOverride:      s.RuntimeLogs.HTTPOverride,
			HTTPMaxFileBytes:  int64(s.RuntimeLogs.HTTPMaxFileMB) * storagepolicy.MiB,
			HTTPRetentionDays: s.RuntimeLogs.HTTPRetentionDays,
			HTTPMaxFiles:      s.RuntimeLogs.HTTPMaxFiles,
		},
		SessionHistory: storagepolicy.HistoryPolicy{
			FilePolicy: storagepolicy.FilePolicy{
				MaxFileBytes:  int64(s.SessionHistory.MaxFileMB) * storagepolicy.MiB,
				RetentionDays: s.SessionHistory.RetentionDays,
				MaxFiles:      s.SessionHistory.MaxFiles,
				MaxTotalBytes: int64(s.SessionHistory.MaxTotalMB) * storagepolicy.MiB,
			},
			WarningBytes: int64(s.SessionHistory.WarningMB) * storagepolicy.MiB,
		},
		ExecutionDetails: storagepolicy.DetailPolicy{
			FilePolicy: storagepolicy.FilePolicy{
				MaxFileBytes:  int64(s.ExecutionDetails.MaxFileMB) * storagepolicy.MiB,
				RetentionDays: s.ExecutionDetails.RetentionDays,
				MaxFiles:      s.ExecutionDetails.MaxFiles,
				MaxTotalBytes: int64(s.ExecutionDetails.MaxTotalMB) * storagepolicy.MiB,
			},
			MaxTurnBytes: int64(s.ExecutionDetails.MaxTurnMB) * storagepolicy.MiB,
		},
		CronHistory: storagepolicy.CronHistoryPolicy{
			FilePolicy: storagepolicy.FilePolicy{
				MaxFileBytes:  int64(s.CronHistory.MaxFileMB) * storagepolicy.MiB,
				RetentionDays: s.CronHistory.RetentionDays,
				MaxFiles:      s.CronHistory.MaxFiles,
				MaxTotalBytes: int64(s.CronHistory.MaxTotalMB) * storagepolicy.MiB,
			},
			MaxPerTask: s.CronHistory.MaxPerTask,
		},
	}
}
