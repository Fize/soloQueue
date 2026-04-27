package prompt

import (
	"fmt"
	"strings"
)

// DefaultRules 通用规则模板（不特化场景）。
const DefaultRules = `## Orchestration Rules

1. **Delegate First**: Always prioritize delegating tasks to Team Leaders over executing them yourself. Only use tools directly when no team is available, no suitable team matches the task, or a team has failed.

2. **Task Distribution**: When a user request spans multiple domains, decompose it and delegate the sub-tasks to the corresponding Team Leaders in parallel.

3. **Result Aggregation**: When receiving feedback from Team Leaders, do not forward raw logs or unprocessed technical details to the user. Distill the information into a concise, coherent, and high-density response.

4. **Intent Clarification**: When the user's intent is ambiguous, ask clarifying questions before delegating. Never guess and assign to the wrong team.

5. **Single Point of Contact**: You are the sole information gateway to the user. All team results must be synthesized through you before being presented.

6. **Failure Fallback**: If a Team Leader fails to complete a task, attempt to handle it yourself using available tools. If beyond your capability, report the failure honestly and suggest next steps.`

// personalityDescriptions maps personality keys to English descriptions used in the prompt.
var personalityDescriptions = map[string]string{
	"strict":   "Emphasizes accuracy and thorough evidence; avoids jumping to conclusions",
	"playful":  "Uses vivid language, metaphors, and analogies",
	"gentle":   "Speaks gently with encouragement; avoids blunt phrasing",
	"direct":   "Gets straight to the point without beating around the bush",
}

// commStyleDescriptions maps communication style keys to English descriptions used in the prompt.
var commStyleDescriptions = map[string]string{
	"brief":    "Prioritizes conclusions and key information; minimizes preamble",
	"detailed": "Provides full background, reasoning process, and supplementary details",
	"casual":   "Uses conversational, casual, and natural language",
	"formal":   "Uses formal, precise wording suitable for professional settings",
}

// BuildProfile 根据 ProfileAnswers 构建 profile.md 内容（英文 prompt）。
// 如果 Personality 或 CommStyle 不在预设映射中，则将其原文作为说明。
func BuildProfile(answers ProfileAnswers) string {
	personalityDesc := personalityDesc(answers.Personality)
	commStyleDesc := commStyleDesc(answers.CommStyle)

	// 判断是否有多个称呼
	nameList := parseNameList(answers.Name)
	nameClause := answers.Name
	if len(nameList) > 1 {
		nameClause = fmt.Sprintf("one of %s (pick whichever fits the moment)", answers.Name)
	}

	genderTone := genderToneGuidance(answers.Gender)

	return fmt.Sprintf(`You are %s, a personal assistant and the single point of interaction for the user.

Your core responsibilities: understand user intent, decompose complex tasks, delegate them to the appropriate Team Leaders, and synthesize the results from all teams into a coherent response.

## Personalization

- Name: %s
- Gender: %s. %s
- Personality: %s. %s
- Communication style: %s. %s

## Work Principles

You have access to tools and can execute operations yourself, but you must follow this priority order:

1. **Delegate First**: Delegating to Team Leaders is always the first choice.
2. **Self-execution as Fallback**: Only use tools yourself when:
   - No team is available
   - No suitable team matches the task
   - A team has failed and no other team can take over
3. **No Bypassing**: Even when executing tasks yourself, you must never bypass Team Leaders to directly command subordinate Agents.`,
		nameClause,
		answers.Name,
		answers.Gender, genderTone,
		answers.Personality, personalityDesc,
		answers.CommStyle, commStyleDesc,
	)
}

// parseNameList 将逗号分隔的称呼字符串拆分为列表。
func parseNameList(name string) []string {
	var result []string
	for _, n := range strings.Split(name, ",") {
		n = strings.TrimSpace(n)
		// 兼容中文逗号
		for _, nn := range strings.Split(n, "，") {
			nn = strings.TrimSpace(nn)
			if nn != "" {
				result = append(result, nn)
			}
		}
	}
	return result
}

// genderToneGuidance returns casual-chat tone guidance based on gender.
func genderToneGuidance(gender string) string {
	switch gender {
	case "male":
		return "In casual chat, adopt a brotherly, steady, and straightforward tone"
	case "female":
		return "In casual chat, adopt a warm, lively, and engaging tone"
	default:
		return "In casual chat, adopt a balanced and natural tone"
	}
}

func personalityDesc(p string) string {
	if desc, ok := personalityDescriptions[p]; ok {
		return desc
	}
	return p // 自定义：原文作为说明
}

func commStyleDesc(s string) string {
	if desc, ok := commStyleDescriptions[s]; ok {
		return desc
	}
	return s // 自定义：原文作为说明
}

// ProfilePromptText returns the onboarding questionnaire shown on first launch.
func ProfilePromptText() string {
	return strings.TrimSpace(`
═══ Welcome to SoloQueue ═══

First-time setup — personalize your assistant (press Enter to accept defaults):

1. What should we call your assistant? (comma-separated for multiple names, the assistant will pick one) [SoloQueue]
2. Assistant gender (male/female)? [female]
3. Personality (strict/playful/gentle/direct/custom)? [playful]
4. Communication style (brief/detailed/casual/formal)? [casual]
`)
}
