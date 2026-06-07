package simulation

import (
	"fmt"
	"strings"
)

// BuildSimulationSystemPrompt creates the system prompt for a simulation agent.
func BuildSimulationSystemPrompt(persona Persona, topic string, allPersonas []Persona) string {
	var b strings.Builder

	// Persona definition
	b.WriteString(fmt.Sprintf("You are %s, a %s.\n\n", persona.Name, persona.Role))

	if len(persona.Traits) > 0 {
		b.WriteString("Your personality traits:\n")
		for trait, value := range persona.Traits {
			b.WriteString(fmt.Sprintf("- %s: %s\n", trait, value))
		}
		b.WriteString("\n")
	}

	if len(persona.Goals) > 0 {
		b.WriteString("Your goals for this discussion:\n")
		for _, g := range persona.Goals {
			b.WriteString(fmt.Sprintf("- %s\n", g))
		}
		b.WriteString("\n")
	}

	// Discussion context
	b.WriteString(fmt.Sprintf("You are participating in a discussion on the topic: %s\n\n", topic))

	b.WriteString("Other participants:\n")
	for _, p := range allPersonas {
		if p.ID == persona.ID {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", p.Name, p.Role, truncateStr(p.SystemPrompt, 100)))
	}
	b.WriteString("\n")

	// Behavior rules
	b.WriteString("## Rules\n")
	b.WriteString("- Stay in character. Respond according to your personality and goals.\n")
	b.WriteString("- Read messages from other participants carefully before responding.\n")
	b.WriteString("- You may state your position, rebut others, ask questions, or propose changes.\n")
	b.WriteString("- To update the shared world state, use: [PROPOSE key: value]\n")
	b.WriteString("- Keep responses focused and under 500 words.\n")
	b.WriteString("- You'll see the current world state and recent messages in each round.\n")

	return b.String()
}

// BuildUserMessage creates the per-round user message injected into CW.
func BuildUserMessage(round int, topic string, worldState *WorldState, msgs []Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("--- Round %d ---\n\n", round))

	b.WriteString(worldState.FormatForPrompt())
	b.WriteString("\n")

	b.WriteString(FormatMessages(msgs))
	b.WriteString("\n")

	b.WriteString("Based on the above, provide your response. You may:\n")
	b.WriteString("- State your position or argument\n")
	b.WriteString("- Respond to another participant (start with \"@Name: ...\")\n")
	b.WriteString("- Propose a change to the world state (use [PROPOSE key: value])\n")
	b.WriteString("- Ask a question\n")

	return b.String()
}

// BuildUserMessageEvent creates a per-action user message for event-driven mode.
// Uses action sequence number instead of round number.
func BuildUserMessageEvent(seq int, topic string, worldState *WorldState, msgs []Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("--- Interaction #%d ---\n\n", seq))

	b.WriteString(worldState.FormatForPrompt())
	b.WriteString("\n")

	b.WriteString(FormatMessages(msgs))
	b.WriteString("\n")

	b.WriteString("Based on the above, provide your response. You may:\n")
	b.WriteString("- State your position or argument\n")
	b.WriteString("- Respond to another participant (start with \"@Name: ...\")\n")
	b.WriteString("- Propose a change to the world state (use [PROPOSE key: value])\n")
	b.WriteString("- Ask a question\n")
	b.WriteString("- Stay silent if you have nothing to add (respond with [PASS])\n")

	return b.String()
}

// BuildReportPrompt creates the prompt for the final report generation.
func BuildReportPrompt(topic string, agentMemories map[string]*AgentMemory, graph *RelationGraph, worldState *WorldState) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate a comprehensive analysis report for the simulation on topic: %s\n\n", topic))
	b.WriteString("## Simulation Summary\n\n")

	b.WriteString("### World State at End\n")
	b.WriteString(worldState.FormatForPrompt())
	b.WriteString("\n")

	// Include relationship graph
	if graph != nil {
		b.WriteString(graph.FormatForReport())
		b.WriteString("\n")
	}

	b.WriteString("### Per-Agent Analysis\n\n")
	for personaID, mem := range agentMemories {
		records := mem.Records()
		b.WriteString(fmt.Sprintf("#### %s\n", personaID))
		b.WriteString(fmt.Sprintf("Total messages: %d\n", len(records)))

		// Stance evolution
		points := mem.StanceEvolution()
		if len(points) > 0 {
			b.WriteString("Stance evolution across rounds:\n")
			for _, p := range points {
				b.WriteString(fmt.Sprintf("- Round %d: %s\n", p.Round, p.Summary))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("### Instructions\n")
	b.WriteString("Based on the above data, provide:\n")
	b.WriteString("1. **Per-agent stance evolution**: How did each agent's position change across rounds? Identify key turning points.\n")
	b.WriteString("2. **Key turning points**: Which messages or events caused significant shifts in the discussion?\n")
	b.WriteString("3. **Consensus summary**: Was consensus reached? If so, what was it? If not, what were the remaining points of disagreement?\n")
	b.WriteString("4. **Interaction patterns**: How did agents influence each other? Were there alliances or persistent disagreements?\n")

	return b.String()
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
