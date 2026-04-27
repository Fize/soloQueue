package skill

import "time"

// 委托任务的全局时间约束
const (
	// TaskDefaultTimeout 委托任务默认超时
	TaskDefaultTimeout = 5 * time.Minute

	// TaskMaxTimeout 委托任务最大超时
	TaskMaxTimeout = 15 * time.Minute
)
