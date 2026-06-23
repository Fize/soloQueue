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
		// Filter by familiarity and relationships (enemies/rivals are also known)
		if relationshipMgr != nil {
			rel := relationshipMgr.Get(persona.ID, p.ID)
			if rel == nil || (rel.Familiarity <= 0.0 && rel.Kind != RelationRival && rel.Affinity >= 0.0) {
				continue // Skip people we don't know
			}
		}

		// Extract strength-related traits
		strengthInfo := ""
		strengthKeys := []string{"combat_strength", "strength", "power", "武功", "实力", "武力值", "战斗力", "武力", "influence", "地位", "权力", "wealth", "财富"}
		var foundTraits []string
		for _, sk := range strengthKeys {
			if sVal, ok := p.Traits[sk]; ok {
				foundTraits = append(foundTraits, fmt.Sprintf("%s: %s", sk, sVal))
			}
		}
		if len(foundTraits) > 0 {
			strengthInfo = " | " + strings.Join(foundTraits, ", ")
		}

		b.WriteString(fmt.Sprintf("- %s (%s%s): %s\n", p.Name, p.Role, strengthInfo, truncateStr(p.Bio, 100)))
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
		b.WriteString("- **定位与决策一致性**：你的一切决策、言辞与行动必须严格符合你的身份背景、职业、MBTI性格及长期目标。禁止做出脱离设定的矛盾举动。\n")
		b.WriteString("- **人际关系约束**：在与他人互动时，必须考虑你对他们的社会关系（如 Rival 对手、Friend 朋友）与 Affinity 亲密度。对对手或敌意者应保持警惕与对抗，对亲人盟友应合作友好。\n")
		b.WriteString("- **群体冲突与势力评估**：\n")
		b.WriteString("  1. 发起或响应冲突（例如 `[CONFLICT]`）时，必须评估当前区域所有在场智能体的立场和实力差距。\n")
		b.WriteString("  2. 结合你和盟友（Affinity > 0）的实力，对比对方及其盟友的实力总和。若处于群体劣势，你应当妥协、逃跑（`[MOVE]`）或寻求调解；若占据绝对优势，可强行施压。\n")
		b.WriteString("  3. 盟友遭受冲突挑衅时，你应该根据性格积极介入支持（如对敌对者发起冲突动作）。\n")
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
		b.WriteString("重要：因为仿真语言设置为中文，你所有的思维、推理（reasoning）、发言、互动和描述必须完全使用中文。但必须严格保持所有动作括号标签的形式不变（例如：使用 [SAY]:、[SAY @name]:、[MOVE]、[PASS]、[DIE]、[SPAWN]、[RELATION]、[CONFLICT @name]:、[HIDE] 标签本身，不要翻译它们）。\n")
	} else {
		b.WriteString("## Core Rules\n")
		b.WriteString("- You are a character living in a simulated world. Act naturally according to your personality, schedule, goals, and relationships.\n")
		b.WriteString("- **Role & Decision Alignment**: All your decisions, speech, and interactions must strictly align with your role, background, MBTI, and long-term goals. Do not act out of character.\n")
		b.WriteString("- **Relationship Constraints**: You must respect relationship kinds and affinities. Act with caution and hostility towards rivals/enemies, and cooperate with allies/friends.\n")
		b.WriteString("- **Group Conflicts & Faction Assessment**:\n")
		b.WriteString("  1. When deciding to initiate or respond to a conflict (e.g. `[CONFLICT]`), evaluate the presence and strength of all agents in the current zone.\n")
		b.WriteString("  2. Compare the sum of your faction's strength (you + allies with Affinity > 0) against the target's faction strength. Retreat, flee (`[MOVE]`), or seek help if you are outnumbered or outmatched. Dominate or fight back if you have a clear faction advantage.\n")
		b.WriteString("  3. Support your allies if they are involved in a conflict in your zone (e.g. by initiating conflict against their opponent).\n")
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
// personaNameByID maps personaID -> display name (used in citation instructions).
// outline is the pre-generated report outline from BuildOutlinePrompt (empty if not using two-pass).
func BuildReportPrompt(topic string, agentMemories map[string]*AgentMemory, graph *RelationGraph, worldState *WorldState, kgContext string, language string, personaNameByID map[string]string, outline string) string {
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
		b.WriteString("### 各角色言行记录（核心证据）\n\n")
		// Build agent name list for citation instructions
		agentNames := make([]string, 0, len(personaNameByID))
		for id, name := range personaNameByID {
			if name != "" {
				agentNames = append(agentNames, fmt.Sprintf("%s（%s）", name, id))
			} else {
				agentNames = append(agentNames, id)
			}
		}
		for personaID, mem := range agentMemories {
			records := mem.Records()
			displayName := personaID
			if name, ok := personaNameByID[personaID]; ok && name != "" {
				displayName = fmt.Sprintf("%s（%s）", name, personaID)
			}
			b.WriteString(fmt.Sprintf("#### %s\n", displayName))
			b.WriteString(fmt.Sprintf("总记录数: %d\n", len(records)))

			points := mem.StanceEvolution()
			if len(points) > 0 {
				b.WriteString("各轮的立场演变:\n")
				for _, p := range points {
					b.WriteString(fmt.Sprintf("- 第 %d 轮: %s\n", p.Round, p.Summary))
				}
			}
			b.WriteString("\n")
		}

		if outline != "" {
			b.WriteString("### 报告大纲（请按此结构撰写）\n\n")
			b.WriteString(outline)
			b.WriteString("\n")
		}

		b.WriteString("### 报告撰写规范\n\n")
		b.WriteString("你是一位仿真分析专家。你的任务是严格基于下面提供的模拟数据（每个角色的完整言行记录、互动关系图、世界状态快照），撰写一份客观、有据可查的分析报告。你是分析师，不是小说家——所有分析必须严格基于数据。\n\n")
		b.WriteString("## 报告结构（必须严格遵循）\n\n")
		b.WriteString("**一、模拟全景**\n")
		b.WriteString("- 用一段话概括模拟中发生的核心事件和发展脉络\n")
		b.WriteString("- 必须引用至少3个不同角色的具体言行作为支撑\n\n")
		b.WriteString("**二、角色演变**\n")
		b.WriteString("- 对每个角色逐一分析：初始立场 vs 最终立场的变化\n")
		b.WriteString(fmt.Sprintf("- 必须引用的角色: %s\n", strings.Join(agentNames, "、")))
		b.WriteString("- 每个角色的分析必须包含：关键转折时刻（具体轮次和事件）、与其他角色的相互影响\n")
		b.WriteString("- 如果某个角色记录很少，明确说明\"该角色活跃度低，数据不足以分析演变\"\n\n")
		b.WriteString("**三、互动网络**\n")
		b.WriteString("- 互动频率最高的角色对及其互动模式（引用互动图数据）\n")
		b.WriteString("- 是否存在联盟形成、对立加剧或态度趋同\n")
		b.WriteString("- 是否存在\"意见领袖\"效应（某个角色的言行显著影响了他人）\n\n")
		b.WriteString("**四、关键转折点**\n")
		b.WriteString("- 列出2-3个改变模拟走向的关键事件\n")
		b.WriteString("- 每个转折点必须说明：触发因素、受影响角色、后续连锁反应\n")
		b.WriteString("- 引用发生转折的具体轮次和角色言行\n\n")
		b.WriteString("**五、涌现现象与预测**\n")
		b.WriteString("- 模拟中出现了哪些设计者未预料到的行为模式\n")
		b.WriteString("- 基于模拟推演，预测在真实场景下可能的发展方向\n")
		b.WriteString("- 说明模拟的局限性（哪些问题数据无法回答，需要进一步实验）\n\n")
		b.WriteString("## 引用规范（极其重要）\n\n")
		b.WriteString("- 每个主要论断必须至少附带一条引用\n")
		b.WriteString("- 引用格式：> \"角色名（第N轮）：原文内容...\"\n")
		b.WriteString("- 引用必须独立成段，前后各空一行\n")
		b.WriteString("- 禁止改写原文语义（可以适当截断但不能改变意思）\n")
		b.WriteString("- ✅ 正确示例：\n")
		b.WriteString("  张教授从一开始就持批评态度。第1轮中他明确表示反对：\n\n")
		b.WriteString("  > \"张教授（第1轮）：这个方案缺乏可行性论证，仓促上马风险太大。\"\n\n")
		b.WriteString("  到了第12轮，在与李工程师多次互动后，他的立场出现微妙变化：\n\n")
		b.WriteString("  > \"张教授（第12轮）：李工提出的一些技术细节值得认真考虑，但总体方向仍有问题。\"\n\n")
		b.WriteString("  这表明技术性论证比单纯立场宣示更能改变专家的态度。\n\n")
		b.WriteString("- ❌ 错误写法（空洞，无证据）：\n")
		b.WriteString("  张教授的态度从反对变成了支持。这说明专家是可以被说服的。\n\n")
		b.WriteString("## 禁止事项\n\n")
		b.WriteString("❌ 编造数据中不存在的对话或事件\n")
		b.WriteString("❌ 用你的常识或外部知识\"填充\"分析\n")
		b.WriteString("❌ 做出数据无法支撑的概括性结论\n")
		b.WriteString("✅ 如果某个角度数据不足，请明确写\"模拟数据中未体现该方面\"\n")
		b.WriteString("✅ 每个结论后面必须引用具体来源\n\n")
		b.WriteString("请完整使用中文撰写整份报告。")
	} else {
		b.WriteString("### Per-Agent Analysis (Primary Evidence)\n\n")
		// Build agent name list for citation instructions
		agentNames := make([]string, 0, len(personaNameByID))
		for id, name := range personaNameByID {
			if name != "" {
				agentNames = append(agentNames, fmt.Sprintf("%s (%s)", name, id))
			} else {
				agentNames = append(agentNames, id)
			}
		}
		for personaID, mem := range agentMemories {
			records := mem.Records()
			displayName := personaID
			if name, ok := personaNameByID[personaID]; ok && name != "" {
				displayName = fmt.Sprintf("%s (%s)", name, personaID)
			}
			b.WriteString(fmt.Sprintf("#### %s\n", displayName))
			b.WriteString(fmt.Sprintf("Total records: %d\n", len(records)))

			points := mem.StanceEvolution()
			if len(points) > 0 {
				b.WriteString("Stance evolution across rounds:\n")
				for _, p := range points {
					b.WriteString(fmt.Sprintf("- Round %d: %s\n", p.Round, p.Summary))
				}
			}
			b.WriteString("\n")
		}

		if outline != "" {
			b.WriteString("### Report Outline (follow this structure)\n\n")
			b.WriteString(outline)
			b.WriteString("\n")
		}

		b.WriteString("### Report Writing Guidelines\n\n")
		b.WriteString("You are a simulation analysis expert. Your task is to write an objective, evidence-based analysis report strictly from the simulation data provided below (every agent's full action/speech records, interaction graph, world state snapshot). You are an analyst, not a novelist — all analysis must be grounded in the data.\n\n")
		b.WriteString("## Required Report Structure\n\n")
		b.WriteString("**1. Simulation Overview**\n")
		b.WriteString("- Summarize the core events and narrative arc in one paragraph\n")
		b.WriteString("- Must cite specific actions/speech from at least 3 different agents\n\n")
		b.WriteString("**2. Agent Evolution**\n")
		b.WriteString("- For each agent: initial stance vs final stance (cite specific rounds)\n")
		b.WriteString(fmt.Sprintf("- Agents to cover: %s\n", strings.Join(agentNames, ", ")))
		b.WriteString("- Each agent analysis must include: key turning point (which round/event), how other agents influenced them\n")
		b.WriteString("- If an agent has very few records, explicitly state: \"Insufficient data to analyze this agent's evolution\"\n\n")
		b.WriteString("**3. Interaction Network**\n")
		b.WriteString("- Which agent pairs interacted most? What patterns emerged?\n")
		b.WriteString("- Evidence of alliance formation, polarization, or convergence\n")
		b.WriteString("- Any \"opinion leader\" effects?\n\n")
		b.WriteString("**4. Key Turning Points**\n")
		b.WriteString("- List 2-3 pivotal events that shifted the simulation's direction\n")
		b.WriteString("- For each: trigger, affected agents, cascade effects\n")
		b.WriteString("- Cite the specific rounds and agent statements\n\n")
		b.WriteString("**5. Emergent Phenomena & Predictions**\n")
		b.WriteString("- Unexpected behavioral patterns\n")
		b.WriteString("- What the simulation predicts for real-world scenarios\n")
		b.WriteString("- Simulation limitations (what questions the data cannot answer)\n\n")
		b.WriteString("## Citation Rules (Critical)\n\n")
		b.WriteString("- Every major claim must be backed by at least one citation\n")
		b.WriteString("- Format: > \"Agent Name (Round N): original content...\"\n")
		b.WriteString("- Citations must be standalone paragraphs with blank lines before and after\n")
		b.WriteString("- Do not alter the semantic meaning of cited content\n")
		b.WriteString("- ✅ Good example:\n")
		b.WriteString("  Prof. Zhang was critical from the start. In Round 1 he clearly objected:\n\n")
		b.WriteString("  > \"Prof. Zhang (Round 1): This proposal lacks feasibility analysis. Rushing forward is too risky.\"\n\n")
		b.WriteString("  By Round 12, after repeated interactions with Engineer Li, his stance softened:\n\n")
		b.WriteString("  > \"Prof. Zhang (Round 12): Li raised some valid technical points worth considering.\"\n\n")
		b.WriteString("  This shows that technical arguments shift expert opinion more effectively than positional statements.\n\n")
		b.WriteString("- ❌ Bad (vague, no evidence):\n")
		b.WriteString("  Prof. Zhang changed from opposition to support. This shows experts can be persuaded.\n\n")
		b.WriteString("## Prohibited Actions\n\n")
		b.WriteString("❌ Fabricate conversations or events not present in the data\n")
		b.WriteString("❌ Use external knowledge to \"fill in\" the analysis\n")
		b.WriteString("❌ Make sweeping conclusions unsupported by the data\n")
		b.WriteString("✅ If data is insufficient on a topic, write \"The simulation data does not cover this aspect\"\n")
		b.WriteString("✅ Every conclusion must cite a specific source\n\n")
		b.WriteString("Write the complete report in the simulation language.")
	}

	return b.String()
}

// BuildOutlinePrompt creates a prompt for generating a report outline.
// It uses lightweight data (agent names + record counts + world state) rather
// than full agent memories, so the LLM can plan structure efficiently before
// the heavy full-report generation pass.
func BuildOutlinePrompt(topic string, agentMemories map[string]*AgentMemory, worldState *WorldState, personaNameByID map[string]string, language string) string {
	var b strings.Builder
	if language == "zh" {
		b.WriteString(fmt.Sprintf("为以下主题的仿真生成一份 4-6 节的报告大纲：%s\n\n", topic))
		b.WriteString("### 模拟概况\n\n")
		b.WriteString(fmt.Sprintf("参与角色: %d 个\n", len(agentMemories)))
		for id, mem := range agentMemories {
			name := id
			if n, ok := personaNameByID[id]; ok && n != "" {
				name = fmt.Sprintf("%s（%s）", n, id)
			}
			b.WriteString(fmt.Sprintf("- %s: %d 条行为记录\n", name, len(mem.Records())))
		}
		if worldState != nil {
			b.WriteString("\n### 结束时的世界状态\n")
			b.WriteString(worldState.FormatForPrompt())
		}
		b.WriteString("\n### 任务\n\n")
		b.WriteString("生成一份报告大纲，包含 4-6 节。每节包含：节标题 + 一句话描述该节应覆盖的内容。\n\n")
		b.WriteString("大纲应覆盖：模拟全景、角色演变、互动网络、关键转折点、涌现现象与预测。\n\n")
		b.WriteString("格式：\n")
		b.WriteString("## 报告大纲\n")
		b.WriteString("### 第一节：标题\n")
		b.WriteString("描述：该节应覆盖的内容...\n")
		b.WriteString("### 第二节：标题\n")
		b.WriteString("描述：...\n\n")
		b.WriteString("只输出大纲，不要输出任何其他内容。")
	} else {
		b.WriteString(fmt.Sprintf("Generate a 4-6 section report outline for the simulation on topic: %s\n\n", topic))
		b.WriteString("### Simulation Overview\n\n")
		b.WriteString(fmt.Sprintf("Participating agents: %d\n", len(agentMemories)))
		for id, mem := range agentMemories {
			name := id
			if n, ok := personaNameByID[id]; ok && n != "" {
				name = fmt.Sprintf("%s (%s)", n, id)
			}
			b.WriteString(fmt.Sprintf("- %s: %d action records\n", name, len(mem.Records())))
		}
		if worldState != nil {
			b.WriteString("\n### End-of-Simulation World State\n")
			b.WriteString(worldState.FormatForPrompt())
		}
		b.WriteString("\n### Task\n\n")
		b.WriteString("Generate a report outline with 4-6 sections. Each section: title + one-sentence description of what it should cover.\n\n")
		b.WriteString("The outline should cover: simulation overview, agent evolution, interaction network, key turning points, emergent phenomena & predictions.\n\n")
		b.WriteString("Format:\n")
		b.WriteString("## Report Outline\n")
		b.WriteString("### Section 1: Title\n")
		b.WriteString("Description: what this section should cover...\n")
		b.WriteString("### Section 2: Title\n")
		b.WriteString("Description: ...\n\n")
		b.WriteString("Output only the outline, nothing else.")
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

