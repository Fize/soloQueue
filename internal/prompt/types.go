package prompt

// LeaderInfo 描述一个可用的 Team Leader。
// 主 Agent 只需知道每个团队"能做什么"（Description），
// 不需要知道团队有什么工具——工具是实现细节，由团队内部自行管理。
type LeaderInfo struct {
	Name             string     // 例如 "dev"
	Description      string     // 例如 "全栈开发工程师，负责前后端开发与架构设计"
	Group            string     // 例如 "DevOps"
	GroupDescription string     // 团队描述（来自 group 文件的 body）
	MatchedWorkspace *Workspace // 按当前 cwd 匹配的 workspace，可能为 nil
}

// ProfileAnswers 用户对个性化问卷的回答。
type ProfileAnswers struct {
	Name        string // 称呼，默认 "SoloQueue"
	Gender      string // 性别，默认 "中性"
	Personality string // 性格，默认 "直接"
	CommStyle   string // 沟通偏好，默认 "简短"
}

// DefaultProfileAnswers 返回全默认值的 ProfileAnswers。
func DefaultProfileAnswers() ProfileAnswers {
	return ProfileAnswers{
		Name:        "SoloQueue",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "casual",
	}
}

// ProfileNeededError profile.md 缺失时返回此错误，
// 由调用方处理交互式问卷流程。
type ProfileNeededError struct {
	RoleID string
}

func (e *ProfileNeededError) Error() string {
	return "profile.md not found for role: " + e.RoleID
}
