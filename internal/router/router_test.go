package router

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func TestLocalClassifierOnlyShortCircuitsStrongEvidence(t *testing.T) {
	c := NewLocalClassifier()
	for _, tt := range []struct { input string; want tasktype.TaskType; matched bool }{
		{"run go test ./...", tasktype.Engineering, true},
		{"搜索最新信息并给官方来源", tasktype.Research, true},
		{"翻译下面这段文字", tasktype.General, true},
		{"继续", tasktype.Unknown, false},
		{"设计一个缓存方案", tasktype.Unknown, false},
	} {
		got := c.Classify(tt.input)
		if got.Matched != tt.matched || got.TaskType != tt.want {
			t.Errorf("Classify(%q) = %+v, want matched=%v type=%s", tt.input, got, tt.matched, tt.want)
		}
	}
}
