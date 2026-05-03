package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

type agentCounts struct {
	l1, l2, l3           int
	run, idle, off, stop int
}

func (s sidebar) AgentSummary(width int) string {
	c := s.counts()
	text := fmt.Sprintf("Agents L1:%d L2:%d L3:%d RUN:%d IDLE:%d OFF:%d", c.l1, c.l2, c.l3, c.run, c.idle, c.off)
	return paneStyle(width).Render(truncate(text, max(width-2, 1)))
}

func (s sidebar) AgentRail(width, height int) string {
	return s.renderAgentTree(width, height, false)
}

func (s sidebar) AgentInspector(width, height int, m model, showAgents bool) string {
	var b strings.Builder

	if showAgents {
		b.WriteString(paneTitleStyle.Render(" AGENTS ") + "\n")
		b.WriteString(s.renderAgentTreeContent(width, height/2, true))
		b.WriteString("\n")
	}

	b.WriteString(paneTitleStyle.Render(" RUNTIME ") + "\n")
	phase := "ready"
	if m.loading {
		phase = "initializing..."
	} else if m.isGenerating {
		phase = m.genPhase.String()
	}
	b.WriteString(kvLine("phase", phase, width))
	if m.promptTokens > 0 || m.outputTokens > 0 {
		b.WriteString(kvLine("tokens", fmt.Sprintf("↓%s ↑%s", formatTokenCount(m.promptTokens), formatTokenCount(m.outputTokens)), width))
	}
	if m.cacheHitTokens > 0 || m.cacheMissTokens > 0 {
		b.WriteString(kvLine("cache", fmt.Sprintf("%s/%s", formatTokenCount(m.cacheHitTokens), formatTokenCount(m.cacheMissTokens)), width))
	}
	var pct int
	if m.sess != nil {
		cur, maxTokens, _ := m.sess.CW().TokenUsage()
		if maxTokens > 0 {
			pct = cur * 100 / maxTokens
		}
	}
	b.WriteString(kvLine("context", fmt.Sprintf("%d%%", pct), width))
	return fitLines(paneStyle(width).Render(b.String()), height)
}

func (s sidebar) renderAgentTree(width, height int, compact bool) string {
	var b strings.Builder
	b.WriteString(paneTitleStyle.Render(" TEAM ") + "\n")
	b.WriteString(s.renderAgentTreeContent(width, height-1, compact))
	return fitLines(paneStyle(width).Render(b.String()), height)
}

func (s sidebar) renderAgentTreeContent(width, height int, compact bool) string {
	registered := []*agent.Agent{}
	if s.registry != nil {
		registered = s.registry.List()
	}
	l2IDs := make(map[string]bool)
	l3IDs := make(map[string]bool)
	for _, sv := range s.supervisors {
		if sv == nil {
			continue
		}
		if a := sv.Agent(); a != nil {
			l2IDs[a.Def.ID] = true
		}
		for _, child := range sv.Children() {
			l3IDs[child.Def.ID] = true
		}
	}

	var l1 []*agent.Agent
	for _, a := range registered {
		if !l2IDs[a.Def.ID] && !l3IDs[a.Def.ID] {
			l1 = append(l1, a)
		}
	}

	var b strings.Builder
	b.WriteString(sectionLine("L1 Session Agents") + "\n")
	if len(l1) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, a := range l1 {
		b.WriteString(agentTreeLine(a, "  ", width))
	}

	b.WriteString(sectionLine("L2 Domain Leaders") + "\n")
	if len(s.supervisors) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, sv := range s.supervisors {
		if sv == nil || sv.Agent() == nil {
			continue
		}
		b.WriteString(agentTreeLine(sv.Agent(), "  ", width))
		children := sv.Children()
		if len(children) == 0 {
			b.WriteString(dimStyle.Render("    └─ (no active workers)") + "\n")
			continue
		}
		for i, child := range children {
			prefix := "    ├─ "
			if i == len(children)-1 {
				prefix = "    └─ "
			}
			b.WriteString(agentTreeLine(child, prefix, width))
		}
	}
	return fitLines(b.String(), height)
}

func (s sidebar) counts() agentCounts {
	c := agentCounts{}
	registered := []*agent.Agent{}
	if s.registry != nil {
		registered = s.registry.List()
	}
	l2IDs := make(map[string]bool)
	l3IDs := make(map[string]bool)
	for _, sv := range s.supervisors {
		if sv == nil {
			continue
		}
		if a := sv.Agent(); a != nil {
			c.l2++
			l2IDs[a.Def.ID] = true
			countState(&c, a.State())
		}
		for _, child := range sv.Children() {
			c.l3++
			l3IDs[child.Def.ID] = true
			countState(&c, child.State())
		}
	}
	for _, a := range registered {
		if !l2IDs[a.Def.ID] && !l3IDs[a.Def.ID] {
			c.l1++
			countState(&c, a.State())
		}
	}
	return c
}

func countState(c *agentCounts, s agent.State) {
	switch s {
	case agent.StateProcessing:
		c.run++
	case agent.StateIdle:
		c.idle++
	case agent.StateStopping:
		c.stop++
	default:
		c.off++
	}
}

func agentTreeLine(a *agent.Agent, indent string, width int) string {
	name := a.Def.Name
	if name == "" {
		name = a.Def.ID
	}
	line := indent + agentBadgeStyle(a.State()).Render(stateLabel(a.State())) + " " + lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(truncate(name, max(width-len(indent)-8, 8)))
	model := a.EffectiveModelID()
	if model != "" {
		line += "\n" + dimStyle.Render(indent+"  "+truncate(model, max(width-len(indent)-4, 6)))
	}
	if lvl := a.EffectiveTaskLevel(); lvl != "" {
		line += " " + levelBadgeStyle.Render(compactLevel(lvl))
	}
	return line + "\n"
}

// levelBadgeStyle renders task level as a compact colored badge.
var levelBadgeStyle = lipgloss.NewStyle().
	Foreground(colorText).
	Background(colorPrimary).
	Padding(0, 1)

// compactLevel shortens level labels for sidebar display.
func compactLevel(lvl string) string {
	switch lvl {
	case "L0-Conversation":
		return "L0"
	case "L1-SimpleSingleFile":
		return "L1"
	case "L2-MediumMultiFile":
		return "L2"
	case "L3-ComplexRefactoring":
		return "L3"
	default:
		return lvl
	}
}

func sectionLine(title string) string {
	return lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("▸ " + title)
}

func kvLine(k, v string, width int) string {
	return dimStyle.Render(fmt.Sprintf("  %-8s", k)) + lipgloss.NewStyle().Foreground(colorText).Render(truncate(v, max(width-12, 4))) + "\n"
}

func paneStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(max(width-2, 1)).Padding(0, 1)
}

func fitLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for len(lines) < maxLines {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
