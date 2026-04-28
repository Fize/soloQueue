package agent

// ─── PriorityMailbox ───────────────────────────────────────────────────────

// Priority 区分 mailbox 中的 job 优先级
type Priority int

const (
	PriorityNormal Priority = iota // 普通用户 Ask / Submit
	PriorityHigh                   // 委托回传 / 超时事件 / 取消指令
)

// prioritizedJob 带 Priority 的 job
type prioritizedJob struct {
	priority Priority
	job      job
}

// PriorityMailbox 支持优先级的 mailbox
//
// 核心设计：
//   - 两个 channel：highCh（容量 4）和 normalCh（容量 8）
//   - run goroutine 优先检查 highCh（委托回传优先处理）
//   - 避免委托结果排在普通用户消息后面等待
//
// 与 chan job 的兼容：
//   - Agent 初始化时，priorityMailbox == nil 表示使用普通 chan job
//   - 通过 WithPriorityMailbox() Option 启用优先级模式
type PriorityMailbox struct {
	highCh   chan prioritizedJob
	normalCh chan prioritizedJob
}

// NewPriorityMailbox 创建一个新的 PriorityMailbox
func NewPriorityMailbox() *PriorityMailbox {
	return &PriorityMailbox{
		highCh:   make(chan prioritizedJob, 4),
		normalCh: make(chan prioritizedJob, 8),
	}
}

// SubmitHigh 投递高优先级 job
func (pm *PriorityMailbox) SubmitHigh(jb job) {
	pm.highCh <- prioritizedJob{priority: PriorityHigh, job: jb}
}

// SubmitNormal 投递普通优先级 job
func (pm *PriorityMailbox) SubmitNormal(jb job) {
	pm.normalCh <- prioritizedJob{priority: PriorityNormal, job: jb}
}

// HighCh 返回高优先级 channel（供 run goroutine 消费）
func (pm *PriorityMailbox) HighCh() <-chan prioritizedJob {
	return pm.highCh
}

// NormalCh 返回普通优先级 channel（供 run goroutine 消费）
func (pm *PriorityMailbox) NormalCh() <-chan prioritizedJob {
	return pm.normalCh
}

// Len 返回当前队列深度（近似值，channel 长度非精确锁定）
func (pm *PriorityMailbox) Len() (high, normal int) {
	return len(pm.highCh), len(pm.normalCh)
}
