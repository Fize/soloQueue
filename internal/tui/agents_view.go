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
	teamCount := len(s.orderedGroupNames(s.getSupervisors()))
	total := c.a1 + c.a2 + c.a3
	text := fmt.Sprintf("Teams:%d Agents:%d RUN:%d IDLE:%d OFF:%d", teamCount, total, c.run, c.idle, c.off)
	return paneStyle(width).Render(truncate(text, max(width-2, 1)))
}

func (s sidebar) AgentRail(width, height int) string {
	return s.renderAgentTree(width, height, false)
}

func (s *sidebar) AgentInspector(width, height int, _ model, showAgents bool) string {
	contentW := max(width-2, 1)
	s.ResizeTeamViewport(contentW, height)

	if showAgents {
		agentsContent := paneTitleStyle.Render(" AGENTS ") + "\n" + s.renderAgentTreeContent(width, 0, true)
		s.teamViewport.SetContent(paneStyle(width).Render(agentsContent))
	} else {
		s.teamViewport.SetContent("")
	}

	return s.teamViewport.View()
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
	supervisors := sortSupervisors(s.getSupervisors())

	// Build runtime agent lookup maps keyed by template ID.
	// l2TemplateIDs: IDs of L2 leader agents (from supervisors).
	l2TemplateIDs := make(map[string]bool)
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		if a := sv.Agent(); a != nil {
			l2TemplateIDs[a.Def.ID] = true
		}
	}

	// l3TemplateIDs: IDs of L3 worker agents.
	l3TemplateIDs := make(map[string]bool)
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		for _, child := range sv.Children() {
			l3TemplateIDs[child.Def.ID] = true
		}
	}

	// l1Agents: session agents (not L2 or L3).
	var l1Agents []*agent.Agent
	for _, a := range registered {
		if !l2TemplateIDs[a.Def.ID] && !l3TemplateIDs[a.Def.ID] {
			l1Agents = append(l1Agents, a)
		}
	}

	// agentsByTemplateID: all running agents keyed by their Def.ID for lookup.
	agentsByTemplateID := make(map[string][]*agent.Agent)
	for _, a := range registered {
		agentsByTemplateID[a.Def.ID] = append(agentsByTemplateID[a.Def.ID], a)
	}

	// L3 children by template ID (for multi-instance grouping).
	l3ByTemplate := make(map[string][]*agent.Agent)
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		for _, child := range sv.Children() {
			l3ByTemplate[child.Def.ID] = append(l3ByTemplate[child.Def.ID], child)
		}
	}

	var b strings.Builder

	// ── A1: Session Agent ──────────────────────────────────────────────────
	b.WriteString(sectionLine("A1 Session") + "\n")
	a1Name := s.assistantName
	if a1Name == "" {
		a1Name = "Assistant"
	}
	if len(l1Agents) > 0 {
		// Show with runtime state
		for _, a := range l1Agents {
			b.WriteString(agentTreeLine(a, "  ", width))
		}
	} else {
		// Show static name when no agent is running yet
		b.WriteString(dimStyle.Render("  ● "+truncate(a1Name, max(width-6, 4))) + "\n")
	}

	// ── A2/A3: Teams (grouped by group name) ───────────────────────────────
	b.WriteString(sectionLine("Teams") + "\n")

	if len(s.groups) == 0 && len(supervisors) == 0 {
		b.WriteString(dimStyle.Render("  (none)") + "\n")
	} else {
		// Collect group names in a deterministic order.
		// Use groups from static templates if available, fall back to supervisors.
		groupNames := s.orderedGroupNames(supervisors)

		for _, groupName := range groupNames {
			// Group display name: use GroupFrontmatter.Name if available, else key.
			displayName := groupName
			if gf, ok := s.groups[groupName]; ok && gf.Frontmatter.Name != "" {
				displayName = gf.Frontmatter.Name
			}
			b.WriteString("  " + teamBadgeStyle.Render("📂 "+truncate(displayName, max(width-8, 4))) + "\n")

			// Templates belonging to this group: leader first, then workers.
			leaderTmpls, workerTmpls := s.templatesByGroup(groupName)

			// Render leader templates
			for _, tmpl := range leaderTmpls {
				s.renderTemplateWithInstances(&b, tmpl, "    ", "★ ", width, agentsByTemplateID)
			}

			// Render worker templates
			for _, tmpl := range workerTmpls {
				s.renderTemplateWithInstances(&b, tmpl, "    ", "  ", width, l3ByTemplate)
			}

			// If no static templates, try to render from runtime supervisors
			if len(leaderTmpls) == 0 && len(workerTmpls) == 0 {
				s.renderRuntimeTeam(&b, groupName, "    ", width, supervisors, l3ByTemplate)
			}
		}
	}

	if height <= 0 {
		return b.String()
	}
	return fitLines(b.String(), height)
}

// orderedGroupNames returns a deterministic list of group names.
// It merges groups from static templates and runtime supervisors.
func (s sidebar) orderedGroupNames(supervisors []*agent.Supervisor) []string {
	seen := make(map[string]bool)
	var names []string

	// First from static templates
	for _, tmpl := range s.templates {
		if tmpl.Group != "" && !seen[tmpl.Group] {
			seen[tmpl.Group] = true
			names = append(names, tmpl.Group)
		}
	}

	// Then from runtime supervisors (may have groups not in templates)
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		g := sv.Group()
		// If supervisor has no group, use its agent name as fallback group name
		if g == "" {
			if a := sv.Agent(); a != nil && a.Def.Name != "" {
				g = a.Def.Name
			} else if a := sv.Agent(); a != nil && a.Def.ID != "" {
				g = a.Def.ID
			}
		}
		if g != "" && !seen[g] {
			seen[g] = true
			names = append(names, g)
		}
	}

	sort.Strings(names)
	return names
}

// templatesByGroup returns leader and worker templates for a given group.
func (s sidebar) templatesByGroup(group string) (leaders, workers []agent.AgentTemplate) {
	for _, tmpl := range s.templates {
		if tmpl.Group != group {
			continue
		}
		if tmpl.IsLeader {
			leaders = append(leaders, tmpl)
		} else {
			workers = append(workers, tmpl)
		}
	}
	// Sort by name for deterministic output
	sort.Slice(leaders, func(i, j int) bool { return leaders[i].Name < leaders[j].Name })
	sort.Slice(workers, func(i, j int) bool { return workers[i].Name < workers[j].Name })
	return
}

// renderTemplateWithInstances renders a template line with its runtime instances.
// When no instances are running, shows only the template name (no state badge).
// When multiple instances exist, shows "Name ×N" with expanded instance list.
func (s sidebar) renderTemplateWithInstances(b *strings.Builder, tmpl agent.AgentTemplate, indent, prefix string, width int, agentsByTmplID map[string][]*agent.Agent) {
	instances := agentsByTmplID[tmpl.ID]

	if len(instances) == 0 {
		// Static display: template name only, no state badge
		name := tmpl.Name
		if name == "" {
			name = tmpl.ID
		}
		b.WriteString(dimStyle.Render(indent+prefix+truncate(name, max(width-len(indent)-len(prefix)-2, 4))) + "\n")
		return
	}

	if len(instances) == 1 {
		// Single instance: show full agent line
		b.WriteString(agentTreeLine(instances[0], indent, width))
		return
	}

	// Multiple instances: show "Name ×N" then expanded list
	name := tmpl.Name
	if name == "" {
		name = tmpl.ID
	}
	// Sort instances by InstanceID for deterministic ordering
	sort.Slice(instances, func(i, j int) bool { return instances[i].InstanceID < instances[j].InstanceID })
	b.WriteString(dimStyle.Render(fmt.Sprintf("%s%s%s ×%d", indent, prefix, truncate(name, max(width-len(indent)-len(prefix)-10, 4)), len(instances))) + "\n")
	for _, a := range instances {
		b.WriteString(agentTreeLine(a, indent+"  ", width))
	}
}

// renderRuntimeTeam renders a team from runtime supervisor data when no static templates exist.
func (s sidebar) renderRuntimeTeam(b *strings.Builder, group, indent string, width int, supervisors []*agent.Supervisor, l3ByTemplate map[string][]*agent.Agent) {
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		// Match group using same fallback logic as orderedGroupNames
		g := sv.Group()
		if g == "" {
			if a := sv.Agent(); a != nil && a.Def.Name != "" {
				g = a.Def.Name
			} else if a := sv.Agent(); a != nil && a.Def.ID != "" {
				g = a.Def.ID
			}
		}
		if g != group {
			continue
		}
		if a := sv.Agent(); a != nil {
			b.WriteString(agentTreeLine(a, indent, width))
		}
		for _, child := range sv.Children() {
			b.WriteString(agentTreeLine(child, indent+"  ", width))
		}
	}
}

func (s sidebar) counts() agentCounts {
	c := agentCounts{}
	registered := []*agent.Agent{}
	if s.registry != nil {
		registered = s.registry.List()
	}

	// Template IDs that belong to L2 leaders.
	l2TemplateIDs := make(map[string]bool)
	for _, sv := range s.getSupervisors() {
		if sv == nil {
			continue
		}
		if a := sv.Agent(); a != nil {
			l2TemplateIDs[a.Def.ID] = true
		}
		// Count all L3 children (now a list per template).
		for _, child := range sv.Children() {
			c.a3++
			countState(&c, child.State())
		}
	}

	// Template IDs for L3 workers.
	l3TemplateIDs := make(map[string]bool)
	for _, sv := range s.getSupervisors() {
		if sv == nil {
			continue
		}
		for _, child := range sv.Children() {
			l3TemplateIDs[child.Def.ID] = true
		}
	}

	for _, a := range registered {
		if l2TemplateIDs[a.Def.ID] {
			c.a2++
			countState(&c, a.State())
		} else if !l3TemplateIDs[a.Def.ID] {
			c.a1++
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
	// Show short InstanceID suffix for disambiguation when multiple instances exist.
	shortID := a.InstanceID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	line := indent + agentBadgeStyle(a.State()).Render(stateLabel(a.State())) + " " + lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(truncate(name, max(width-len(indent)-16, 8)))
	line += " " + dimStyle.Render(shortID)
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
