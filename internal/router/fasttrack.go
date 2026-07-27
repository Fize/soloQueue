package router

import (
	"regexp"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

// LocalClassifier is deliberately a high-precision shortcut. Ambiguous input
// returns Matched=false and is left to the semantic classifier.
type LocalClassifier struct {
	codeBlock    *regexp.Regexp
	stackTrace   *regexp.Regexp
	filePath     *regexp.Regexp
	command      *regexp.Regexp
	research     *regexp.Regexp
	general      *regexp.Regexp
	engineering  *regexp.Regexp
}

type LocalResult struct {
	TaskType   tasktype.TaskType
	Matched    bool
	ReasonCode string
}

func NewLocalClassifier() *LocalClassifier {
	return &LocalClassifier{
		codeBlock:   regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_+-]+)?\\n.+?```"),
		stackTrace:  regexp.MustCompile(`(?i)(panic:|stack trace|traceback|exception|fatal error:|\bat .*:\d+)`),
		filePath:    regexp.MustCompile(`(?i)(?:^|\s)(?:[\w.-]+/)*[\w.-]+\.(?:go|ts|tsx|js|jsx|py|java|rs|sql|yaml|yml|json|md)(?:\b|$)`),
		command:     regexp.MustCompile(`(?i)\b(go test|go build|pnpm |npm |git |docker |kubectl |pytest|cargo |make |sql)\b`),
		research:    regexp.MustCompile(`(?i)(搜索|联网|查最新|官方文档|引用来源|事实核查|竞品调研|市场调研|最新信息|latest|current|official (docs|documentation)|cite sources|fact.?check|research)`),
		general:     regexp.MustCompile(`(?i)^(你好|您好|谢谢|thanks|hello|hi\b)|(?:翻译|润色|改写|总结).*(?:下面|这段|文本|文字|文章)|(?:translate|rewrite|summari[sz]e)\b`),
		engineering: regexp.MustCompile(`(?i)(修复|重构|实现|编译|测试|部署|调试|迁移|增加索引|改代码|改造|fix|refactor|implement|compile|test|deploy|debug|migration|index)`),
	}
}

func (c *LocalClassifier) Classify(text string) LocalResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return LocalResult{TaskType: tasktype.General, Matched: true, ReasonCode: "general.empty"}
	}
	hasEngineeringStrong := c.codeBlock.MatchString(text) || c.stackTrace.MatchString(text) || c.command.MatchString(text)
	if !hasEngineeringStrong && c.filePath.MatchString(text) && c.engineering.MatchString(text) {
		hasEngineeringStrong = true
	}
	hasResearchStrong := c.research.MatchString(text)
	hasGeneralStrong := c.general.MatchString(text)

	if hasEngineeringStrong && !hasResearchStrong && !hasGeneralStrong {
		return LocalResult{TaskType: tasktype.Engineering, Matched: true, ReasonCode: "engineering.strong"}
	}
	if hasResearchStrong && !hasEngineeringStrong && !hasGeneralStrong {
		return LocalResult{TaskType: tasktype.Research, Matched: true, ReasonCode: "research.sources"}
	}
	if hasGeneralStrong && !hasEngineeringStrong && !hasResearchStrong {
		return LocalResult{TaskType: tasktype.General, Matched: true, ReasonCode: "general.explicit"}
	}
	return LocalResult{}
}
