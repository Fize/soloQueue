package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

type agentCounts struct {
	a1, a2, a3           int
	run, idle, off, stop int
}

func (s sidebar) AgentSummary(width int) string {
	c := s.counts()
	text := fmt.Sprintf("Agents A1:%d A2:%d A3:%d RUN:%d IDLE:%d OFF:%d", c.a1, c.a2, c.a3, c.run, c.idle, c.off)
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
	if totalErrs, lastErr := s.aggregateErrors(); totalErrs > 0 {
		b.WriteString(errorStyle.Render(fmt.Sprintf("  %d error(s)", totalErrs)))
		if lastErr != "" {
			b.WriteString(dimStyle.Render(" — " + truncate(lastErr, max(width-16, 10))))
		}
		b.WriteString("\n")
	}
	return fitLines(paneStyle(width).Render(b.String()), height)
}

func (s sidebar) renderAgentTree(width, height int, compact bool) string {
	var b strings.Builder
	b.WriteString(paneTitleStyle.Render(" TEAM ") + "\n")
	b.WriteString(s.renderAgentTreeContent(width, height-1, compact))
	return fitLines(paneStyle(width).Render(b.String()), height)
}

func sortSupervisors(supervisors []*agent.Supervisor) []*agent.Supervisor {
	sorted := make([]*agent.Supervisor, len(supervisors))
	copy(sorted, supervisors)
	sort.Slice(sorted, func(i, j int) bool {
		ni, nj := "", ""
		if a := sorted[i].Agent(); a != nil {
			ni = a.Def.Name
		}
		if a := sorted[j].Agent(); a != nil {
			nj = a.Def.Name
		}
		if ni != nj {
			return ni < nj
		}
		return ni < nj
	})
	return sorted
}

func (s sidebar) renderAgentTreeContent(width, height int, compact bool) string {
	registered := []*agent.Agent{}
	if s.registry != nil {
		registered = s.registry.List()
	}
	supervisors := sortSupervisors(s.supervisors)
	l2IDs := make(map[string]bool)
	l3IDs := make(map[string]bool)
	for _, sv := range supervisors {
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

	// Collect A3 workers from all supervisors
	var a3 []*agent.Agent
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		a3 = append(a3, sv.Children()...)
	}

	var b strings.Builder
	b.WriteString(sectionLine("A1 Session Agents") + "\n")
	if len(l1) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, a := range l1 {
		b.WriteString(agentTreeLine(a, "  ", width))
	}

	b.WriteString(sectionLine("A2 Domain Leaders") + "\n")
	if len(supervisors) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, sv := range supervisors {
		if sv == nil || sv.Agent() == nil {
			continue
		}
		b.WriteString(agentTreeLine(sv.Agent(), "  ", width))
	}

	b.WriteString(sectionLine("A3 Workers") + "\n")
	if len(a3) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, child := range a3 {
		b.WriteString(agentTreeLine(child, "  ", width))
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
			c.a2++
			l2IDs[a.Def.ID] = true
			countState(&c, a.State())
		}
		for _, child := range sv.Children() {
			c.a3++
			l3IDs[child.Def.ID] = true
			countState(&c, child.State())
		}
	}
	for _, a := range registered {
		if !l2IDs[a.Def.ID] && !l3IDs[a.Def.ID] {
			c.a1++
			countState(&c, a.State())
		}
	}
	return c
}

func (s sidebar) aggregateErrors() (total int32, last string) {
	seen := make(map[string]bool)
	for _, a := range s.allAgents() {
		if seen[a.Def.ID] {
			continue
		}
		seen[a.Def.ID] = true
		if ec := a.ErrorCount(); ec > 0 {
			total += ec
			if le := a.LastError(); le != "" {
				last = le
			}
		}
	}
	return
}

func (s sidebar) allAgents() []*agent.Agent {
	var out []*agent.Agent
	if s.registry != nil {
		out = append(out, s.registry.List()...)
	}
	for _, sv := range s.supervisors {
		if sv == nil {
			continue
		}
		if a := sv.Agent(); a != nil {
			out = append(out, a)
		}
		out = append(out, sv.Children()...)
	}
	return out
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
	if ec := a.ErrorCount(); ec > 0 {
		line += " " + errorStyle.Render(fmt.Sprintf("✗%d", ec))
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
