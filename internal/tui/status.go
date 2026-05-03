package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── /status rendering ──────────────────────────────────────────────────────

// renderStatus 渲染 /status 命令输出
//
// 层级判定：
//   - L2 = Supervisor.Agent()
//   - L3 = Supervisor.Children()
//   - L1 = Registry 中排除 L2/L3 的其余 Agent
func renderStatus(registry *agent.Registry, supervisors []*agent.Supervisor) string {
	var sb strings.Builder

	sb.WriteString(clearStatusStyle.Render("◆  Agent Status") + "\n\n")

	if registry == nil || registry.Len() == 0 {
		sb.WriteString(dimStyle.Render("  No agents registered") + "\n\n")
		return sb.String()
	}

	// 收集 L2/L3 的 ID，用于过滤 L1
	l2IDs := make(map[string]bool)
	l3IDs := make(map[string]bool)
	for _, sv := range supervisors {
		if a := sv.Agent(); a != nil {
			l2IDs[a.Def.ID] = true
		}
		for _, child := range sv.Children() {
			l3IDs[child.Def.ID] = true
		}
	}

	// L1 = Registry 中排除 L2/L3
	var l1Agents []*agent.Agent
	for _, a := range registry.List() {
		if !l2IDs[a.Def.ID] && !l3IDs[a.Def.ID] {
			l1Agents = append(l1Agents, a)
		}
	}

	// ── L1 Session Agents ──────────────────────────────────────────────────
	sb.WriteString(sectionHeader("A1 Session Agents"))
	if len(l1Agents) == 0 {
		sb.WriteString(dimStyle.Render("  (none)") + "\n")
	}
	for _, a := range l1Agents {
		sb.WriteString(renderAgentLine(a, "  "))
	}
	sb.WriteString("\n")

	// ── A2 Domain Leaders ──────────────────────────────────────────────────
	if len(supervisors) > 0 {
		sb.WriteString(sectionHeader("A2 Domain Leaders"))
		for _, sv := range supervisors {
			l2 := sv.Agent()
			if l2 == nil {
				continue
			}
			sb.WriteString(renderAgentLine(l2, "  "))
		}
		sb.WriteString("\n")
	}

	// ── A3 Workers ──────────────────────────────────────────────────────────
	var a3Agents []*agent.Agent
	for _, sv := range supervisors {
		if sv != nil {
			a3Agents = append(a3Agents, sv.Children()...)
		}
	}
	if len(supervisors) > 0 {
		sb.WriteString(sectionHeader("A3 Workers"))
		if len(a3Agents) == 0 {
			sb.WriteString(dimStyle.Render("  (none)") + "\n")
		}
		for _, child := range a3Agents {
			sb.WriteString(renderAgentLine(child, "  "))
		}
		sb.WriteString("\n")
	}

	// ── Total ─────────────────────────────────────────────────────────────────
	sb.WriteString(dimStyle.Render(fmt.Sprintf("  Total: %d agents", registry.Len())) + "\n\n")

	return sb.String()
}

// sectionHeader 渲染分组标题
func sectionHeader(title string) string {
	return lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		Render("  ▸ "+title) + "\n"
}

// renderAgentLine 渲染单个 Agent 的状态行
func renderAgentLine(a *agent.Agent, indent string) string {
	state := a.State()
	name := a.Def.Name
	if name == "" {
		name = a.Def.ID
	}

	// 状态标签（带颜色）
	stateText := stateStyle(state).Render(state.String())

	// 元数据片段
	var parts []string
	parts = append(parts, stateText)

	if a.Def.ModelID != "" {
		parts = append(parts, dimStyle.Render(a.Def.ModelID))
	}

	// Mailbox depth（仅非零时显示）
	high, normal := a.MailboxDepth()
	if high > 0 || normal > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("mailbox: %d/%d", high, normal)))
	}

	// Pending delegations（仅非零时显示）
	if pd := a.PendingDelegations(); pd > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(colorInfo).Render(fmt.Sprintf("delegations: %d", pd)))
	}

	nameStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	return indent + nameStyle.Render(name) + "  " + strings.Join(parts, dimStyle.Render(" · ")) + "\n"
}

// stateStyle 返回状态对应的样式
func stateStyle(s agent.State) lipgloss.Style {
	switch s {
	case agent.StateIdle:
		return lipgloss.NewStyle().Foreground(colorAccent) // green
	case agent.StateProcessing:
		return lipgloss.NewStyle().Foreground(colorInfo) // light blue
	case agent.StateStopping:
		return lipgloss.NewStyle().Foreground(colorWarning) // orange-red
	case agent.StateStopped:
		return lipgloss.NewStyle().Foreground(colorMuted) // gray
	default:
		return lipgloss.NewStyle().Foreground(colorMuted)
	}
}
