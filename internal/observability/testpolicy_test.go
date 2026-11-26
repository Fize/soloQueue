package observability

import "github.com/xiaobaitu/soloqueue/internal/storagepolicy"

// bytePolicy builds an execution-details limit set with byte precision so tests
// can stay at the smallest sizes that exercise each bound.
func bytePolicy(total, turn, file int64) storagepolicy.Policy {
	return storagepolicy.Policy{ExecutionDetails: storagepolicy.DetailPolicy{
		FilePolicy:   storagepolicy.FilePolicy{MaxTotalBytes: total, MaxFileBytes: file, RetentionDays: 30},
		MaxTurnBytes: turn,
	}}
}
