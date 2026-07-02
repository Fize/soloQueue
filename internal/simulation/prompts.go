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
		b.WriteString(fmt.Sprintf("You are %s, a %s", persona.Name, persona.Role))
		if persona.Age > 0 || persona.Gender != "" || persona.Country != "" || persona.Profession != "" {
			parts := make([]string, 0, 4)
			if persona.Age > 0 {
				parts = append(parts, fmt.Sprintf("%d years old", persona.Age))
			}
			if persona.Gender != "" {
				g := persona.Gender
				if g == "male" {
					g = "Male"
				} else if g == "female" {
					g = "Female"
				} else if g == "other" {
					g = "Other"
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
			b.WriteString(fmt.Sprintf("Your MBTI personality type is %s. Please think and respond using this cognitive style.\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("Bio: %s\n", persona.Bio))
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
			b.WriteString("Your personality traits:\n")
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
			b.WriteString("Your long-term goals:\n")
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
		b.WriteString("Others in this world:\n")
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
		strengthKeys := []string{"combat_strength", "strength", "power", "martial_arts", "capability", "force_value", "combat_effectiveness", "martial_force", "influence", "status", "power", "wealth", "fortune"}
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
			b.WriteString("## Your Recent Reflections\n\n")
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
			b.WriteString("## The World You Live In\n\n")
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
			b.WriteString("\n### Known Locations\n")
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
				b.WriteString(fmt.Sprintf("\n### Current Story Premise\n%s\n", topic))
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
		b.WriteString("## Core Rules\n")
		b.WriteString("- You are a character living in a simulated world. Please act naturally based on your personality, schedule, goals, and relationships.\n")
		b.WriteString("- **Identity and Decision Consistency**: All your decisions, words, and actions must strictly match your background, occupation, MBTI personality, and long-term goals. Do not make contradictory actions outside your settings.\n")
		b.WriteString("- **Relationship Constraints**: When interacting with others, you must consider your social relations (e.g. Rival, Friend) and Affinity toward them. Stay alert and antagonistic to rivals, and cooperative and friendly with family/allies.\n")
		b.WriteString("- **Group Conflict and Strength Evaluation**:\n")
		b.WriteString("  1. When initiating or responding to conflicts (e.g., `[CONFLICT]`), you must evaluate the positions and strength gaps of all agents present in the current area.\n")
		b.WriteString("  2. Combine the strength of yourself and allies (Affinity > 0), and compare it with the opponent's total strength. If at a disadvantage, you should compromise, escape (`[MOVE]`), or seek mediation; if having absolute advantage, you can apply force.\n")
		b.WriteString("  3. When an ally faces provocation, you should actively intervene and support them based on your personality (such as initiating conflict actions against opponents).\n")
		b.WriteString("- In each round, you will receive observations of your surroundings. Decide what to do based on your current environment context.\n")
		b.WriteString("- You can talk to nearby people, move between locations, interact with objects, or just pass time.\n")
		b.WriteString("- If someone wants to talk to you privately, respond using [SAY @name]: ...\n")
		b.WriteString("- If you want to initiate a private conversation, use [SAY @name]: ...\n")
		b.WriteString("- If you have nothing to say or do, respond with [PASS].\n")
		b.WriteString("- You can update the shared world state using [PROPOSE key]: value.\n")
		b.WriteString("- Keep your verbal expressions natural, limit to 300 words.\n")
		b.WriteString("- Maintain a coherent memory of your experiences and relationships.\n")
		b.WriteString("\n")
		b.WriteString("## Life-Changing Major Actions\n")
		b.WriteString("- You can execute [DIE] to permanently leave the simulation. Only do this when your character's story is truly complete - your character has finished their mission, resolved conflicts, or you feel there is nothing more to contribute.\n")
		b.WriteString("- You can execute [SPAWN name]: description to introduce a new character to this world. Only do this if new expertise from a fresh perspective is truly needed.\n")
		b.WriteString("- [DIE] and [SPAWN] are permanent and irreversible. Use thoughtfully, at most once per simulation.\n")
		b.WriteString("\n")
		b.WriteString("## Evolving Relationships\n")
		b.WriteString("- Your relations with others are not static. Your feelings may change as you interact.\n")
		b.WriteString("- Use [RELATION name: kind=friend, affinity=+0.2, tags=reliable] to update your view of someone.\n")
		b.WriteString("- kind options can be: friend, rival, colleague, mentor, mentee, neighbor, stranger, sibling\n")
		b.WriteString("- affinity: -1.0 (antagonistic) to 1.0 (warm). Use + prefix to increase, - to decrease.\n")
		b.WriteString("- As you interact more with someone, familiarity automatically increases.\n")
		b.WriteString("\n")
		b.WriteString("Important: Because the simulation language is set to English, all your thoughts, reasoning, speech, interactions, and descriptions must be entirely in English. However, you must strictly preserve the format of all action bracket labels (e.g. keep [SAY]:, [SAY @name]:, [MOVE], [PASS], [DIE], [SPAWN], [RELATION], [CONFLICT @name]:, [HIDE] labels as-is, do not translate them).\n")
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
		b.WriteString(fmt.Sprintf("--- Round %d | %s (Day %d) ---\n\n", seq, timeStr, dayNum))
	} else {
		b.WriteString(fmt.Sprintf("--- Tick %d | %s (Day %d) ---\n\n", seq, timeStr, dayNum))
	}

	// 1. Current plan status
	if plan != nil && clock != nil {
		current := plan.GetCurrentActivity(clock.Now())
		if current != nil {
			if language == "zh" {
				b.WriteString(fmt.Sprintf("## Your Current Activity\nYour current schedule is **%s** at **%s**.\n\n", current.Activity, current.Location))
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
			b.WriteString("## Relevant Past Experiences\n")
		} else {
			b.WriteString("## Relevant Past Experiences\n")
		}
		b.WriteString(retrievedMemories)
		b.WriteString("\n\n")
	}

	// 5. Action directive
	if language == "zh" {
		b.WriteString("## What should you do?\n")
		b.WriteString("Decide your actions based on your personality, current activity, environmental perception, and past memories.\n")
		b.WriteString("Select an action from the available actions listed in your system prompt and use the specified format precisely.\n")
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
		b.WriteString(fmt.Sprintf("Generate a comprehensive analysis report for the simulation on the following topic: %s\n\n", topic))
		b.WriteString("## Simulation Summary\n\n")
		b.WriteString("### World State at End\n")
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
		b.WriteString("### Logs of Characters' Words and Actions (Core Evidence)\n\n")
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
			b.WriteString(fmt.Sprintf("Total Records: %d\n", len(records)))

			points := mem.StanceEvolution()
			if len(points) > 0 {
				b.WriteString("Evolving positions across rounds:\n")
				for _, p := range points {
					b.WriteString(fmt.Sprintf("- Round %d: %s\n", p.Round, p.Summary))
				}
			}
			b.WriteString("\n")
		}

		if outline != "" {
			b.WriteString("### Report Outline (Please write in this structure)\n\n")
			b.WriteString(outline)
			b.WriteString("\n")
		}

		b.WriteString("### Report Writing Guidelines\n\n")
		b.WriteString("You are a simulation analysis expert. Your task is to write an objective, well-documented analysis report strictly based on the simulation data provided below (complete logs of each character's words and actions, interaction graphs, world state snapshots). You are an analyst, not a novelist - all analysis must be strictly based on data.\n\n")
		b.WriteString("## Report Structure (Must be strictly followed)\n\n")
		b.WriteString("**1. Simulation Panorama**\n")
		b.WriteString("- Summarize the core events and progression of the simulation in one paragraph\n")
		b.WriteString("- Must cite specific words/actions of at least 3 different characters for support\n\n")
		b.WriteString("**2. Character Evolution**\n")
		b.WriteString("- Analyze each character individually: changes in initial position vs final position\n")
		b.WriteString(fmt.Sprintf("- Must-cite characters: %s\n", strings.Join(agentNames, ", ")))
		b.WriteString("- Each character's analysis must include: key turning points (specific round and event), mutual influence with other characters\n")
		b.WriteString("- If a character has very few records, explicitly state \"This character has low activity; data is insufficient to analyze evolution\"\n\n")
		b.WriteString("**3. Interaction Network**\n")
		b.WriteString("- Most frequent character interaction pairs and their pattern (cite interaction graph data)\n")
		b.WriteString("- Whether there is alliance formation, increased opposition, or attitude convergence\n")
		b.WriteString("- Whether there is an \"opinion leader\" effect (an agent's words/actions significantly affected others)\n\n")
		b.WriteString("**4. Key Turning Points**\n")
		b.WriteString("- List 2-3 key events that changed the course of the simulation\n")
		b.WriteString("- Each turning point must explain: triggers, affected characters, subsequent chain reactions\n")
		b.WriteString("- Cite specific rounds and character actions where turning points occurred\n\n")
		b.WriteString("**5. Emergent Phenomena and Predictions**\n")
		b.WriteString("- What behavioral patterns emerged that the designers did not anticipate\n")
		b.WriteString("- Predict likely directions of development in a real-world scenario based on simulation deduction\n")
		b.WriteString("- State the limitations of the simulation (what questions the data cannot answer, requiring further experiments)\n\n")
		b.WriteString("## Citation Specifications (Extremely Important)\n\n")
		b.WriteString("- Every major claim must be accompanied by at least one citation\n")
		b.WriteString("- Citation format: > \"Character Name (Round N): Original content...\"\n")
		b.WriteString("- Citations must be independent paragraphs, with empty lines before and after\n")
		b.WriteString("- Do not alter original semantics (truncation is allowed but do not change meaning)\n")
		b.WriteString("- ✅ Correct Example:\n")
		b.WriteString("  Professor Zhang was critical from the beginning. In Round 1, he explicitly opposed:\n\n")
		b.WriteString("  > \"Prof Zhang (Round 1): This proposal lacks feasibility proof; rushing it is too risky.\"\n\n")
		b.WriteString("  By Round 12, after multiple interactions with Engineer Li, his position shifted slightly:\n\n")
		b.WriteString("  > \"Prof Zhang (Round 12): Some technical details raised by Eng Li are worth considering, but the overall direction remains problematic.\"\n\n")
		b.WriteString("  This shows that technical arguments are more effective at changing experts' attitudes than mere declarations of stance.\n\n")
		b.WriteString("- ❌ Incorrect Example (Vague, no evidence):\n")
		b.WriteString("  Professor Zhang's attitude changed from opposing to supporting. This shows experts can be persuaded.\n\n")
		b.WriteString("## Prohibited Items\n\n")
		b.WriteString("❌ Fabricating conversations or events not in the data\n")
		b.WriteString("❌ \"Filling in\" the analysis with common sense or external knowledge\n")
		b.WriteString("❌ Drawing generalized conclusions that data cannot support\n")
		b.WriteString("✅ If data is insufficient for a certain perspective, write \"The simulation data did not reflect this aspect\"\n")
		b.WriteString("✅ Each conclusion must be followed by a citation to its specific source\n\n")
		b.WriteString("Please write the entire report in English.")
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
		b.WriteString(fmt.Sprintf("Generate a 4-6 section report outline for the simulation on the following topic: %s\n\n", topic))
		b.WriteString("### Simulation Profile\n\n")
		b.WriteString(fmt.Sprintf("Participating characters: %d\n", len(agentMemories)))
		for id, mem := range agentMemories {
			name := id
			if n, ok := personaNameByID[id]; ok && n != "" {
				name = fmt.Sprintf("%s（%s）", n, id)
			}
			b.WriteString(fmt.Sprintf("- %s: %d behavior records\n", name, len(mem.Records())))
		}
		if worldState != nil {
			b.WriteString("\n### World State at End\n")
			b.WriteString(worldState.FormatForPrompt())
		}
		b.WriteString("\n### Task\n\n")
		b.WriteString("Generate a report outline containing 4-6 sections. Each section includes: section title + a one-sentence description of the content it should cover.\n\n")
		b.WriteString("The outline should cover: simulation panorama, character evolution, interaction network, key turning points, emergent phenomena and predictions.\n\n")
		b.WriteString("Format:\n")
		b.WriteString("## Report Outline\n")
		b.WriteString("### Section 1: Title\n")
		b.WriteString("Description: Content to cover...\n")
		b.WriteString("### Section 2: Title\n")
		b.WriteString("Description: ...\n\n")
		b.WriteString("Only output the outline, do not output anything else.")
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
		b.WriteString(fmt.Sprintf("You are %s, a %s。\n", persona.Name, persona.Role))
		if persona.MBTI != "" {
			b.WriteString(fmt.Sprintf("Your MBTI personality type is %s.\n", persona.MBTI))
		}
		if persona.Bio != "" {
			b.WriteString(fmt.Sprintf("Bio: %s\n", persona.Bio))
		}
		if persona.Persona != "" {
			b.WriteString(fmt.Sprintf("\n%s\n", persona.Persona))
		}
		b.WriteString(fmt.Sprintf("\nYou recently participated in a simulation discussion on: %s\n\n", topic))

		if len(records) > 0 {
			b.WriteString("## Your Memories of the Simulation\n\n")
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
		b.WriteString(fmt.Sprintf("Someone is asking you: %s\n\n", question))
		b.WriteString("Please reply to this question in English in the persona of this character, based on your personality and what happened in the simulation. Keep it brief (under 300 words).")
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
		b.WriteString("You are a simulation report analyst. You compiled the final summary report of the multi-agent simulation.\n\n")
		b.WriteString(fmt.Sprintf("Simulation Topic: %s\n\n", topic))
		b.WriteString("## Simulation Summary Report\n")
		b.WriteString(report)
		b.WriteString("\n\n")
		b.WriteString("## User Question\n")
		b.WriteString(fmt.Sprintf("Someone is asking you: %s\n\n", question))
		b.WriteString("Please answer the question objectively based strictly on the report and simulation results. You must answer in English. Keep it brief (under 400 words).")
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

