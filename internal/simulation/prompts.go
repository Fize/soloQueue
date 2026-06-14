package simulation

import (
	"fmt"
	"strings"
	"time"
)

// BuildGenerativeAgentSystemPrompt creates the system prompt for a Generative Agents-style agent.
// Unlike the original discussion-participant prompt, this prompt creates a complete autonomous
// character that perceives, remembers, plans, and acts within a simulated environment.
func BuildGenerativeAgentSystemPrompt(
	persona Persona,
	allPersonas []Persona,
	env *Environment,
	plan *DailyPlan,
	relationshipMgr *RelationshipManager,
	reflections []ReflectionRecord,
	personaNameByID map[string]string,
	clock *SimClock,
) string {
	var b strings.Builder

	// 1. Persona identity (same as before but adapted for autonomous life)
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
	b.WriteString("\n")

	// Traits
	if len(persona.Traits) > 0 {
		b.WriteString("Your personality traits:\n")
		for trait, value := range persona.Traits {
			if len(trait) < 7 || trait[:7] != "stance:" {
				b.WriteString(fmt.Sprintf("- %s: %s\n", trait, value))
			}
		}
		b.WriteString("\n")
	}

	// Goals
	if len(persona.Goals) > 0 {
		b.WriteString("Your long-term goals:\n")
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
		b.WriteString(plan.FormatForPrompt(clock.Now()))
		b.WriteString("\n")
	}

	// 4. Other people (personas)
	b.WriteString("Other people in this world:\n")
	for _, p := range allPersonas {
		if p.ID == persona.ID {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", p.Name, p.Role, truncateStr(p.Bio, 100)))
	}
	b.WriteString("\n")

	// 5. Relationship state
	if relationshipMgr != nil {
		b.WriteString(relationshipMgr.FormatForPrompt(persona.ID, personaNameByID))
		b.WriteString("\n")
	}

	// 6. Recent reflections (highest-level abstractions)
	if len(reflections) > 0 {
		b.WriteString("## Your Recent Reflections\n\n")
		for _, r := range reflections {
			if len(r.Content) > 300 {
				b.WriteString(fmt.Sprintf("- %s...\n", r.Content[:300]))
			} else {
				b.WriteString(fmt.Sprintf("- %s\n", r.Content))
			}
		}
		b.WriteString("\n")
	}

	// 7. Action syntax
	b.WriteString(FormatActionsForPrompt())
	b.WriteString("\n")

	// 8. Behavior rules
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

	return b.String()
}

// BuildTickUserMessage creates the per-tick user message for the Generative Agents loop.
func BuildTickUserMessage(seq int, observations []Observation, worldState *WorldState, retrievedMemories string, plan *DailyPlan, clock *SimClock) string {
	var b strings.Builder

	// Time header
	timeStr := "??:??"
	dayNum := 0
	if clock != nil {
		timeStr = clock.TimeString()
		dayNum = clock.Day()
	}
	b.WriteString(fmt.Sprintf("--- Tick %d | %s (Day %d) ---\n\n", seq, timeStr, dayNum))

	// 1. Current plan status
	if plan != nil && clock != nil {
		current := plan.GetCurrentActivity(clock.Now())
		if current != nil {
			b.WriteString(fmt.Sprintf("## Your Current Activity\nYou are scheduled to be **%s** at **%s**.\n\n", current.Activity, current.Location))
		}
	} else if plan != nil {
		b.WriteString(plan.FormatForPrompt(time.Now()))
		b.WriteString("\n")
	}

	// 2. Observations (environment + messages)
	b.WriteString(FormatObservations(observations))
	b.WriteString("\n")

	// 3. World state
	if worldState != nil {
		b.WriteString(worldState.FormatForPrompt())
		b.WriteString("\n")
	}

	// 4. Retrieved relevant memories
	if retrievedMemories != "" {
		b.WriteString("## Relevant Past Experiences\n")
		b.WriteString(retrievedMemories)
		b.WriteString("\n\n")
	}

	// 5. Action directive
	b.WriteString("## What do you do?\n")
	b.WriteString("Based on your personality, current activity, perceptions, and memories, decide on your action.\n")
	b.WriteString("Choose ONE action from the available actions listed in your system prompt.\n")

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
func BuildReportPrompt(topic string, agentMemories map[string]*AgentMemory, graph *RelationGraph, worldState *WorldState, kgContext string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate a comprehensive analysis report for the simulation on topic: %s\n\n", topic))
	b.WriteString("## Simulation Summary\n\n")

	b.WriteString("### World State at End\n")
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

	return b.String()
}

// BuildReplayPrompt creates the prompt for post-simulation agent questioning.
func BuildReplayPrompt(persona *Persona, topic string, records []MemoryRecord, question string) string {
	var b strings.Builder

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

	return b.String()
}

// BuildReportAnalystPrompt creates the prompt for post-simulation report questioning.
func BuildReportAnalystPrompt(topic string, report string, question string) string {
	var b strings.Builder

	b.WriteString("You are the Simulation Report Analyst. You compiled the final summary report for a multi-agent simulation.\n\n")
	b.WriteString(fmt.Sprintf("Simulation Topic: %s\n\n", topic))
	b.WriteString("## Simulation Summary Report\n")
	b.WriteString(report)
	b.WriteString("\n\n")
	b.WriteString("## User Question\n")
	b.WriteString(fmt.Sprintf("A user is asking you now: %s\n\n", question))
	b.WriteString("Answer the question objectively, based strictly on the report and the simulation outcomes. Be concise (under 400 words).")

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
	ga := BuildGenerativeAgentSystemPrompt(persona, allPersonas, nil, nil, nil, nil, nil, nil)

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
	return BuildTickUserMessage(round, observations, worldState, "", nil, nil)
}

