package simulation

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuildGenerativeAgentSystemPrompt creates the system prompt for a Generative Agents-style agent.
// Unlike the original discussion-participant prompt, this prompt creates a complete autonomous
// character that perceives, remembers, plans, and acts within a simulated environment.
func BuildGenerativeAgentSystemPrompt(
	language string,
	persona Persona,
	allPersonas []Persona,
	env *Environment,
	plan *DailyPlan,
	relationshipMgr *RelationshipManager,
	reflections []ReflectionRecord,
	personaNameByID map[string]string,
	clock *SimClock,
	worldState map[string]any,
) string {
	var b strings.Builder

	// 1. Persona identity (same as before but adapted for autonomous life)
	if language == "zh" {
		b.WriteString(fmt.Sprintf("你是 %s，一个 %s", persona.Name, persona.Role))
		if persona.Age > 0 || persona.Gender != "" || persona.Country != "" || persona.Profession != "" {
			parts := make([]string, 0, 4)
			if persona.Age > 0 {
				parts = append(parts, fmt.Sprintf("%d 岁", persona.Age))
			}
			if persona.Gender != "" {
				g := persona.Gender
				if g == "male" {
					g = "男"
				} else if g == "female" {
					g = "女"
				} else if g == "other" {
					g = "其他"
				}
				parts = append(parts, g)
			}
			if persona.Country != "" {
				parts = append(parts, persona.Country)
			}
			if persona.Profession != "" {
				parts = append(parts, persona.Profession)
			}
			b.WriteString("（" + strings.Join(parts, "，") + "）")
		}
		b.WriteString("。\n")
		if persona.MBTI != "" {
			b.WriteString(fmt.Sprintf("你的 MBTI 性格类型是 %s。请以此认知风格进行思考和回应。\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("简介：%s\n", persona.Bio))
		}
		if persona.Persona != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", persona.Persona))
		}
	} else {
		b.WriteString(fmt.Sprintf("You are %s, a %s", persona.Name, persona.Role))
		if persona.Age > 0 || persona.Gender != "" || persona.Country != "" || persona.Profession != "" {
			parts := make([]string, 0, 4)
			if persona.Age > 0 {
				parts = append(parts, fmt.Sprintf("age %d", persona.Age))
			}
			if persona.Gender != "" {
				parts = append(parts, persona.Gender)
			}
			if persona.Country != "" {
				parts = append(parts, persona.Country)
			}
			if persona.Profession != "" {
				parts = append(parts, persona.Profession)
			}
			b.WriteString(" (" + strings.Join(parts, ", ") + ")")
		}
		b.WriteString(".\n")
		if persona.MBTI != "" {
			b.WriteString(fmt.Sprintf("Your MBTI personality type is %s. Think and respond in a way consistent with this cognitive style.\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("Bio: %s\n", persona.Bio))
		}
		if persona.Persona != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", persona.Persona))
		}
	}
	b.WriteString("\n")

	// Traits
	if len(persona.Traits) > 0 {
		if language == "zh" {
			b.WriteString("你的性格特质：\n")
		} else {
			b.WriteString("Your personality traits:\n")
		}
		for trait, value := range persona.Traits {
			if len(trait) < 7 || trait[:7] != "stance:" {
				b.WriteString(fmt.Sprintf("- %s: %s\n", trait, value))
			}
		}
		b.WriteString("\n")
	}

	// Goals
	if len(persona.Goals) > 0 {
		if language == "zh" {
			b.WriteString("你的长期目标：\n")
		} else {
			b.WriteString("Your long-term goals:\n")
		}
		for _, g := range persona.Goals {
			b.WriteString(fmt.Sprintf("- %s\n", g))
		}
		b.WriteString("\n")
	}

	// 2. Environment layout
	if env != nil {
		b.WriteString(env.FormatForPrompt())
		b.WriteString("\n")
	}

	// 3. Daily plan
	if plan != nil {
		b.WriteString(plan.FormatForPrompt(clock.Now(), language))
		b.WriteString("\n")
	}

	// 4. Other people (personas)
	if language == "zh" {
		b.WriteString("在这个世界里的其他人：\n")
	} else {
		b.WriteString("Other people in this world:\n")
	}
	for _, p := range allPersonas {
		if p.ID == persona.ID {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", p.Name, p.Role, truncateStr(p.Bio, 100)))
	}
	b.WriteString("\n")

	// 5. Relationship state
	if relationshipMgr != nil {
		b.WriteString(relationshipMgr.FormatForPrompt(persona.ID, personaNameByID, language))
		b.WriteString("\n")
	}

	// 6. Recent reflections (highest-level abstractions)
	if len(reflections) > 0 {
		if language == "zh" {
			b.WriteString("## 你最近的反思\n\n")
		} else {
			b.WriteString("## Your Recent Reflections\n\n")
		}
		for _, r := range reflections {
			if len(r.Content) > 300 {
				b.WriteString(fmt.Sprintf("- %s...\n", r.Content[:300]))
			} else {
				b.WriteString(fmt.Sprintf("- %s\n", r.Content))
			}
		}
		b.WriteString("\n")
	}

	// 7. World context (seed-derived world rules, locations, premise)
	if worldState != nil && len(worldState) > 0 {
		if language == "zh" {
			b.WriteString("## 你所在的世界\n\n")
		} else {
			b.WriteString("## The World You Live In\n\n")
		}
		// Separate seed metadata from display content
		displayKeys := make([]string, 0, len(worldState))
		for k := range worldState {
			if !strings.HasPrefix(k, "_seed_") {
				displayKeys = append(displayKeys, k)
			}
		}
		sort.Strings(displayKeys)
		for _, k := range displayKeys {
			v := worldState[k]
			b.WriteString(fmt.Sprintf("- **%s**: %v\n", k, formatValue(v)))
		}
		// Show seed locations if available
		if locsRaw, ok := worldState["_seed_locations"]; ok {
			b.WriteString("\n### 已知地点\n")
			if locs, ok := locsRaw.([]any); ok {
				for _, l := range locs {
					if lm, ok := l.(map[string]any); ok {
						b.WriteString(fmt.Sprintf("- **%s**: %s\n", lm["name"], lm["desc"]))
					}
				}
			}
		}
		// Show seed topic as world premise
		if topic, ok := worldState["_seed_topic"]; ok {
			if language == "zh" {
				b.WriteString(fmt.Sprintf("\n### 当前故事前提\n%s\n", topic))
			} else {
				b.WriteString(fmt.Sprintf("\n### Current Story Premise\n%s\n", topic))
			}
		}
		b.WriteString("\n")
	}

	// 8. Action syntax
	b.WriteString(FormatActionsForPromptInLanguage(language))
	b.WriteString("\n")

	// 8. Behavior rules
	if language == "zh" {
		b.WriteString("## 核心规则\n")
		b.WriteString("- 你是生活在模拟世界中的一个角色。请根据你的性格、日程安排、目标和人际关系自然地行动。\n")
		b.WriteString("- 每一轮你都会收到关于周围环境的观察。根据你当前的环境背景决定要做什么。\n")
		b.WriteString("- 你可以和附近的人交谈、在不同地点之间移动、与物体互动，或者仅仅是度过时间。\n")
		b.WriteString("- 如果有人想私下与你交谈，请使用 [SAY @name]: ... 来回复他们。\n")
		b.WriteString("- 如果你想发起私下交谈，请使用 [SAY @name]: ...\n")
		b.WriteString("- 如果你没有什么好说或好做的，请用 [PASS] 回应。\n")
		b.WriteString("- 你可以通过 [PROPOSE key]: value 来更新共享的世界状态。\n")
		b.WriteString("- 保持你口头表达的信息自然，字数限制在300字以内。\n")
		b.WriteString("- 维护你对经历和人际关系的连贯记忆。\n")
		b.WriteString("\n")
		b.WriteString("## 改变一生的重大动作\n")
		b.WriteString("- 你可以执行 [DIE] 来永久离开仿真。仅在你的角色故事真正完整时才执行此操作——你的角色已完成使命，你已解决冲突，或者你觉得没有更多可以贡献的了。\n")
		b.WriteString("- 你可以执行 [SPAWN name]: description 来在这个世界中引入一个新角色。仅在确实需要没有的、全新视角的专业知识时才执行此操作。\n")
		b.WriteString("- [DIE] 和 [SPAWN] 是永久性的，无法撤销。请深思熟虑地使用，每次仿真最多使用一次。\n")
		b.WriteString("\n")
		b.WriteString("## 演变的人际关系\n")
		b.WriteString("- 你与他人的关系不是一成不变的。随着你的互动，你的感受可能会改变。\n")
		b.WriteString("- 使用 [RELATION name: kind=friend, affinity=+0.2, tags=reliable] 来更新你对某人的看法。\n")
		b.WriteString("- kind 选项可以是: friend, rival, colleague, mentor, mentee, neighbor, stranger, sibling\n")
		b.WriteString("- affinity: -1.0 (敌对) 到 1.0 (温暖)。使用 + 前缀表示相对增加，- 表示减少。\n")
		b.WriteString("- 随着你与某人互动增多，熟悉度 (familiarity) 会自动增加。\n")
		b.WriteString("\n")
		b.WriteString("重要：因为仿真语言设置为中文，你所有的思维、推理（reasoning）、发言、互动和描述必须完全使用中文。但必须严格保持所有动作括号标签的形式不变（例如：使用 [SAY]:、[SAY @name]:、[MOVE]、[PASS]、[DIE]、[SPAWN]、[RELATION] 标签本身，不要翻译它们）。\n")
	} else {
		b.WriteString("## Core Rules\n")
		b.WriteString("- You are a character living in a simulated world. Act naturally according to your personality, schedule, goals, and relationships.\n")
		b.WriteString("- Each turn, you will receive observations about your surroundings. Decide what to do based on your current context.\n")
		b.WriteString("- You may speak to people nearby, move between locations, interact with objects, or simply pass the time.\n")
		b.WriteString("- If someone wants to speak with you privately, respond to them using [SAY @name]: ...\n")
		b.WriteString("- If you want to initiate a private conversation, use [SAY @name]: ...\n")
		b.WriteString("- If you have nothing to say or do, respond with [PASS].\n")
		b.WriteString("- You may update the shared world state with [PROPOSE key]: value.\n")
		b.WriteString("- Keep your spoken messages natural and under 300 words.\n")
		b.WriteString("- Maintain a consistent memory of your experiences and relationships.\n")
		b.WriteString("\n")
		b.WriteString("## Life-Changing Actions\n")
		b.WriteString("- You may [DIE] to permanently leave the simulation. Only do this when your character's story is truly complete — your role is finished, you've resolved conflicts, or you feel you have nothing more to contribute.\n")
		b.WriteString("- You may [SPAWN name]: description to introduce a new character into the world. Only do this when a genuinely new perspective or expertise is needed that no existing character provides.\n")
		b.WriteString("- [DIE] and [SPAWN] are permanent and cannot be undone. Use them thoughtfully, at most once per simulation.\n")
		b.WriteString("\n")
		b.WriteString("## Evolving Relationships\n")
		b.WriteString("- Your relationships with others are not static. As you interact, your feelings can change.\n")
		b.WriteString("- Use [RELATION name: kind=friend, affinity=+0.2, tags=reliable] to update how you feel about someone.\n")
		b.WriteString("- kind can be: friend, rival, colleague, mentor, mentee, neighbor, stranger, sibling\n")
		b.WriteString("- affinity: -1.0 (hostile) to 1.0 (warm). Use + prefix for relative increase, - for decrease.\n")
		b.WriteString("- familiarity increases automatically as you interact with someone more.\n")
	}

	return b.String()
}

// BuildTickUserMessage creates the per-tick user message for the Generative Agents loop.
func BuildTickUserMessage(seq int, observations []Observation, worldState *WorldState, retrievedMemories string, plan *DailyPlan, clock *SimClock, language string) string {
	var b strings.Builder

	// Time header
	timeStr := "??:??"
	dayNum := 0
	if clock != nil {
		timeStr = clock.TimeString()
		dayNum = clock.Day()
	}
	if language == "zh" {
		b.WriteString(fmt.Sprintf("--- 轮次 %d | %s (第 %d 天) ---\n\n", seq, timeStr, dayNum))
	} else {
		b.WriteString(fmt.Sprintf("--- Tick %d | %s (Day %d) ---\n\n", seq, timeStr, dayNum))
	}

	// 1. Current plan status
	if plan != nil && clock != nil {
		current := plan.GetCurrentActivity(clock.Now())
		if current != nil {
			if language == "zh" {
				b.WriteString(fmt.Sprintf("## 你当前的活动\n你当前的日程是 **%s**，在 **%s**。\n\n", current.Activity, current.Location))
			} else {
				b.WriteString(fmt.Sprintf("## Your Current Activity\nYou are scheduled to be **%s** at **%s**.\n\n", current.Activity, current.Location))
			}
		}
	} else if plan != nil {
		b.WriteString(plan.FormatForPrompt(time.Now(), language))
		b.WriteString("\n")
	}

	// 2. Observations (environment + messages)
	b.WriteString(FormatObservations(observations, language))
	b.WriteString("\n")

	// 3. World state
	if worldState != nil {
		b.WriteString(worldState.FormatForPrompt())
		b.WriteString("\n")
	}

	// 4. Retrieved relevant memories
	if retrievedMemories != "" {
		if language == "zh" {
			b.WriteString("## 相关的过往经历\n")
		} else {
			b.WriteString("## Relevant Past Experiences\n")
		}
		b.WriteString(retrievedMemories)
		b.WriteString("\n\n")
	}

	// 5. Action directive
	if language == "zh" {
		b.WriteString("## 你该做什么？\n")
		b.WriteString("根据你的性格、当前活动、环境感知和过往记忆，决定你的行动。\n")
		b.WriteString("从你的系统提示词（system prompt）中列出的可用动作中选择一个动作并精确使用指定格式。\n")
	} else {
		b.WriteString("## What do you do?\n")
		b.WriteString("Based on your personality, current activity, perceptions, and memories, decide on your action.\n")
		b.WriteString("Choose ONE action from the available actions listed in your system prompt.\n")
	}

	return b.String()
}

// BuildRetrievalQuery creates a search query for the MemoryEngine based on current context.
func BuildRetrievalQuery(persona *Persona, observations []Observation, currentPlan *PlanItem, clock *SimClock) string {
	var b strings.Builder

	b.WriteString(persona.Name + " ")
	if currentPlan != nil {
		b.WriteString(currentPlan.Activity + " ")
	}
	for _, o := range observations {
		if len(o.Content) > 100 {
			b.WriteString(o.Content[:100] + " ")
		} else {
			b.WriteString(o.Content + " ")
		}
	}

	return strings.TrimSpace(b.String())
}

// BuildReportPrompt creates the prompt for the final report generation.
func BuildReportPrompt(topic string, agentMemories map[string]*AgentMemory, graph *RelationGraph, worldState *WorldState, kgContext string, language string) string {
	var b strings.Builder
	if language == "zh" {
		b.WriteString(fmt.Sprintf("为以下主题的仿真生成一份全面的分析报告： %s\n\n", topic))
		b.WriteString("## 仿真总结\n\n")
		b.WriteString("### 结束时的世界状态\n")
	} else {
		b.WriteString(fmt.Sprintf("Generate a comprehensive analysis report for the simulation on topic: %s\n\n", topic))
		b.WriteString("## Simulation Summary\n\n")
		b.WriteString("### World State at End\n")
	}
	b.WriteString(worldState.FormatForPrompt())
	b.WriteString("\n")

	if kgContext != "" {
		b.WriteString(kgContext)
		b.WriteString("\n")
	}

	if graph != nil {
		b.WriteString(graph.FormatForReport())
		b.WriteString("\n")
	}

	if language == "zh" {
		b.WriteString("### 各智能体分析\n\n")
		for personaID, mem := range agentMemories {
			records := mem.Records()
			b.WriteString(fmt.Sprintf("#### %s\n", personaID))
			b.WriteString(fmt.Sprintf("总记录数: %d\n", len(records)))

			points := mem.StanceEvolution()
			if len(points) > 0 {
				b.WriteString("各轮的立场演变:\n")
				for _, p := range points {
					b.WriteString(fmt.Sprintf("- %s\n", p.Summary))
				}
			}
			b.WriteString("\n")
		}

		b.WriteString("### 报告撰写指南\n")
		b.WriteString("根据以上数据，提供：\n")
		b.WriteString("1. **每个角色的演变**：每个角色的行为和人际关系是如何演变的？\n")
		b.WriteString("2. **关键转折点**：哪些事件导致了重大的转变？\n")
		b.WriteString("3. **互动模式**：角色之间是如何相互影响的？\n")
		b.WriteString("4. **涌现行为**：是否有任何令人惊讶的或新出现的社会现象？\n\n")
		b.WriteString("请完全使用中文撰写整份报告。")
	} else {
		b.WriteString("### Per-Agent Analysis\n\n")
		for personaID, mem := range agentMemories {
			records := mem.Records()
			b.WriteString(fmt.Sprintf("#### %s\n", personaID))
			b.WriteString(fmt.Sprintf("Total records: %d\n", len(records)))

			points := mem.StanceEvolution()
			if len(points) > 0 {
				b.WriteString("Stance evolution across rounds:\n")
				for _, p := range points {
					b.WriteString(fmt.Sprintf("- %s\n", p.Summary))
				}
			}
			b.WriteString("\n")
		}

		b.WriteString("### Instructions\n")
		b.WriteString("Based on the above data, provide:\n")
		b.WriteString("1. **Per-agent evolution**: How did each agent's behavior and relationships evolve?\n")
		b.WriteString("2. **Key turning points**: Which events caused significant shifts?\n")
		b.WriteString("3. **Interaction patterns**: How did agents influence each other?\n")
		b.WriteString("4. **Emergent behaviors**: Any surprising or emergent social phenomena?\n")
	}

	return b.String()
}

// BuildReplayPrompt creates the prompt for post-simulation agent questioning.
func BuildReplayPrompt(persona *Persona, topic string, records []MemoryRecord, question string, language string) string {
	var b strings.Builder

	if language == "zh" {
		b.WriteString(fmt.Sprintf("你是 %s，一个 %s。\n", persona.Name, persona.Role))
		if persona.MBTI != "" {
			b.WriteString(fmt.Sprintf("你的 MBTI 性格类型是 %s。\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("简介：%s\n", persona.Bio))
		}
		if persona.Persona != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", persona.Persona))
		}
		b.WriteString(fmt.Sprintf("\n你最近参与了一场关于以下主题的仿真讨论： %s\n\n", topic))

		if len(records) > 0 {
			b.WriteString("## 你对仿真的记忆\n\n")
			start := 0
			if len(records) > 10 {
				start = len(records) - 10
			}
			recent := records[start:]
			for _, rec := range recent {
				if rec.Role == "user" || rec.Role == "observation" {
					b.WriteString(fmt.Sprintf("你观察到：\n%s\n\n", truncateStr(rec.Content, 300)))
				} else {
					b.WriteString(fmt.Sprintf("你做过/说过：\n%s\n\n", truncateStr(rec.Content, 300)))
				}
			}
		}

		b.WriteString("## 当前提问\n")
		b.WriteString(fmt.Sprintf("有人正在问你：%s\n\n", question))
		b.WriteString("请模仿该角色的口吻，根据你的性格以及仿真中发生的事情，使用中文回答这个问题。保持简洁（300字以内）。")
	} else {
		b.WriteString(fmt.Sprintf("You are %s, a %s.\n", persona.Name, persona.Role))
		if persona.MBTI != "" {
			b.WriteString(fmt.Sprintf("Your MBTI personality type is %s.\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("Bio: %s\n", persona.Bio))
		}
		if persona.Persona != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", persona.Persona))
		}
		b.WriteString(fmt.Sprintf("\nYou recently participated in a simulation on the topic: %s\n\n", topic))

		if len(records) > 0 {
			b.WriteString("## Your Memory of the Simulation\n\n")
			start := 0
			if len(records) > 10 {
				start = len(records) - 10
			}
			recent := records[start:]
			for _, rec := range recent {
				if rec.Role == "user" || rec.Role == "observation" {
					b.WriteString(fmt.Sprintf("You observed:\n%s\n\n", truncateStr(rec.Content, 300)))
				} else {
					b.WriteString(fmt.Sprintf("You did/said:\n%s\n\n", truncateStr(rec.Content, 300)))
				}
			}
		}

		b.WriteString("## Current Question\n")
		b.WriteString(fmt.Sprintf("Someone is asking you now: %s\n\n", question))
		b.WriteString("Answer in-character, based on your personality and what happened in the simulation. Be concise (under 300 words).")
	}

	return b.String()
}

// BuildReportAnalystPrompt creates the prompt for post-simulation report questioning.
func BuildReportAnalystPrompt(topic string, report string, question string, language string) string {
	var b strings.Builder

	if language == "zh" {
		b.WriteString("你是仿真报告分析师。你编译了多智能体仿真的最终总结报告。\n\n")
		b.WriteString(fmt.Sprintf("仿真主题: %s\n\n", topic))
		b.WriteString("## 仿真总结报告\n")
		b.WriteString(report)
		b.WriteString("\n\n")
		b.WriteString("## 用户问题\n")
		b.WriteString(fmt.Sprintf("用户正在问你：%s\n\n", question))
		b.WriteString("请严格根据报告和仿真结果，客观地回答该问题。你必须使用中文回答。保持简洁（400字以内）。")
	} else {
		b.WriteString("You are the Simulation Report Analyst. You compiled the final summary report for a multi-agent simulation.\n\n")
		b.WriteString(fmt.Sprintf("Simulation Topic: %s\n\n", topic))
		b.WriteString("## Simulation Summary Report\n")
		b.WriteString(report)
		b.WriteString("\n\n")
		b.WriteString("## User Question\n")
		b.WriteString(fmt.Sprintf("A user is asking you now: %s\n\n", question))
		b.WriteString("Answer the question objectively, based strictly on the report and the simulation outcomes. Be concise (under 400 words).")
	}

	return b.String()
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ─── Backward compatibility aliases ──────────────────────────────────────────

// BuildSimulationSystemPrompt is a compatibility wrapper.
func BuildSimulationSystemPrompt(persona Persona, topic string, allPersonas []Persona) string {
	ga := BuildGenerativeAgentSystemPrompt("en", persona, allPersonas, nil, nil, nil, nil, nil, nil, nil)

	var b strings.Builder
	b.WriteString(ga)
	b.WriteString(fmt.Sprintf("\nYou are participating in a discussion on the topic: %s\n", topic))

	// Moderator-specific rules for backward compatibility
	isMediator := persona.Traits["role_type"] == "mediator" ||
		strings.Contains(strings.ToLower(persona.Role), "mediator") ||
		strings.Contains(strings.ToLower(persona.Role), "moderator") ||
		strings.Contains(strings.ToLower(persona.Role), "host")

	if isMediator {
		b.WriteString("- You are the Moderator/Host. Stay neutral, guide the discussion, ask questions to silent participants, resolve conflicts, and summarize consensus.\n")
		b.WriteString("- Do not take a strong personal stance; instead, facilitate the group's deliberation.\n")
	}

	return b.String()
}

// BuildUserMessage is a compatibility wrapper. The new architecture uses
// BuildTickUserMessage instead.
func BuildUserMessage(round int, topic string, worldState *WorldState, msgs []Message) string {
	observations := make([]Observation, 0, len(msgs))
	for _, m := range msgs {
		observations = append(observations, Observation{
			Type:    "agent_speak",
			Content: m.Content,
			Source:  m.From,
		})
	}
	return BuildTickUserMessage(round, observations, worldState, "", nil, nil, "en")
}

