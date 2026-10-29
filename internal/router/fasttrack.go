package router

import (
	"regexp"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

// LocalClassifier is deliberately a high-precision shortcut. Ambiguous input
// returns Matched=false and is left to the semantic classifier.
type LocalClassifier struct {
	codeBlock         *regexp.Regexp
	stackTrace        *regexp.Regexp
	filePath          *regexp.Regexp
	command           *regexp.Regexp
	research          *regexp.Regexp
	general           *regexp.Regexp
	engineering       *regexp.Regexp
	engineeringEntity *regexp.Regexp
	engineeringAction *regexp.Regexp
	engineeringDefect *regexp.Regexp
}

type LocalResult struct {
	TaskType   tasktype.TaskType
	Matched    bool
	ReasonCode string
}

func NewLocalClassifier() *LocalClassifier {
	return &LocalClassifier{
		codeBlock:         regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_+-]+)?\\n.+?```"),
		stackTrace:        regexp.MustCompile(`(?i)(panic:|stack trace|traceback|exception|fatal error:|\bat .*:\d+)`),
		filePath:          regexp.MustCompile(`(?i)(?:^|\s)(?:[\w.-]+/)*[\w.-]+\.(?:go|ts|tsx|js|jsx|py|java|rs|sql|yaml|yml|json|md)(?:\b|$)`),
		command:           regexp.MustCompile(`(?i)\b(go test|go build|pnpm |npm |git |docker |kubectl |pytest|cargo |make |sql)\b`),
		research:          regexp.MustCompile(routerLanguage.Fasttrack.Research),
		general:           regexp.MustCompile(routerLanguage.Fasttrack.General),
		engineering:       regexp.MustCompile(routerLanguage.Fasttrack.Engineering),
		engineeringEntity: regexp.MustCompile(routerLanguage.Fasttrack.EngineeringEntity),
		engineeringAction: regexp.MustCompile(routerLanguage.Fasttrack.EngineeringAction),
		engineeringDefect: regexp.MustCompile(routerLanguage.Fasttrack.EngineeringDefect),
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
	if !hasEngineeringStrong && c.engineeringEntity.MatchString(text) &&
		(c.engineeringAction.MatchString(text) || c.engineeringDefect.MatchString(text)) {
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
