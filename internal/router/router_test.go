package router

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func TestLocalClassifierOnlyShortCircuitsStrongEvidence(t *testing.T) {
	c := NewLocalClassifier()
	for _, tt := range []struct {
		input   string
		want    tasktype.TaskType
		matched bool
	}{
		{"run go test ./...", tasktype.Engineering, true},
		{"添加一个只读的 resolve_project 工具", tasktype.Engineering, true},
		{"MCP 错误被当成普通文本", tasktype.Engineering, true},
		{"运行中 agent 不会自动更新", tasktype.Engineering, true},
		{"调查为什么这个功能异常", tasktype.Engineering, true},
		{"分析项目里失败的原因", tasktype.Engineering, true},
		{"inspect the router failure", tasktype.Engineering, true},
		{"排查代码库中的问题", tasktype.Engineering, true},
		{"查看最近的日志", tasktype.Engineering, true},
		{"接口响应异常", tasktype.Engineering, true},
		{"提交代码修改", tasktype.Engineering, true},
		{"查询数据库中的慢 SQL", tasktype.Engineering, true},
		{"新增 API 并更新配置", tasktype.Engineering, true},
		{"定位 session 无法恢复的原因", tasktype.Engineering, true},
		{"接入 WebSocket", tasktype.Engineering, true},
		{"升级 LSP", tasktype.Engineering, true},
		{"trace the wire failure", tasktype.Engineering, true},
		{"update the prompt", tasktype.Engineering, true},
		{"优化模型路由", tasktype.Engineering, true},
		{"搜索最新信息并给官方来源", tasktype.Research, true},
		{"翻译下面这段文字", tasktype.General, true},
		{"继续", tasktype.Unknown, false},
		{"设计一个缓存方案", tasktype.Unknown, false},
		{"分析这篇文章的观点", tasktype.Unknown, false},
		{"调查市场规模", tasktype.Unknown, false},
		{"项目里有哪些业务成员", tasktype.Unknown, false},
		{"提交一份总结", tasktype.Unknown, false},
		{"把功能介绍写得更清楚", tasktype.Unknown, false},
		{"优化商业模型", tasktype.Unknown, false},
		{"update capitalization", tasktype.Unknown, false},
	} {
		got := c.Classify(tt.input)
		if got.Matched != tt.matched || got.TaskType != tt.want {
			t.Errorf("Classify(%q) = %+v, want matched=%v type=%s", tt.input, got, tt.matched, tt.want)
		}
	}
}
