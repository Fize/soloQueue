package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xiaobaitu/soloqueue/internal/skill"
)

// printfOnce prints above the current View without ClearScreen.
// Use only at Init when the cellbuf is still empty — no ghost-content risk.
func printfOnce(format string, args ...any) tea.Cmd {
	return tea.Printf(format, args...)
}

// printfWithClear wraps tea.Printf with a ClearScreen to keep the v2
// cursed renderer's cellbuf in sync. Without ClearScreen after Printf,
// the renderer's diff-based incremental update produces ghost content
// because Printf's insertAbove bypasses the cellbuf.
func printfWithClear(format string, args ...any) tea.Cmd {
	return tea.Sequence(
		tea.Printf(format, args...),
		func() tea.Msg { return tea.ClearScreen() },
	)
}

// ─── Built-in commands ────────────────────────────────────────────────────────

func (m *model) handleBuiltin(input string) (bool, tea.Cmd) {
	name := strings.ToLower(strings.TrimSpace(input))

	// Flush logo to scrollback on first command
	var logoCmd tea.Cmd
	if !m.logoShown {
		m.logoShown = true
		logoCmd = printfOnce("%s", renderLogo(m.cfg.Version))
	}

	var cmd tea.Cmd
	switch name {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		text := agentStyle.Render("Solo:") + "\n" + dimStyle.Render("Commands: /help /clear /status /version /quit")
		// Append available skill slash commands
		if m.cfg.Skills != nil {
			skills := m.cfg.Skills.Skills()
			var userSkills []*skill.Skill
			for _, s := range skills {
				if s.UserInvocable && !s.DisableModelInvocation {
					userSkills = append(userSkills, s)
				}
			}
			if len(userSkills) > 0 {
				text += "\n" + dimStyle.Render("Skills:")
				for _, s := range userSkills {
					text += "\n" + dimStyle.Render("  /"+s.ID) + " — " + s.Description
				}
			}
		}
		text += "\n\n"
		cmd = printfWithClear("%s", text)

	case "/clear":
		// Cancel any active stream
		if m.isGenerating {
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.resetGenState()
			m.current = nil
		}
		// 清空上下文：追加 /clear 事件到 timeline，重置 ContextWindow
		if m.sess != nil {
			_ = m.sess.Clear()
		}
		m.messages = nil
		text := clearStatusStyle.Render("◆  context cleared") + "\n\n"
		cmd = printfWithClear("%s", text)

	case "/version":
		text := agentStyle.Render("Solo:") + "\n" + lipgloss.NewStyle().Bold(true).Render("SoloQueue "+m.cfg.Version) + "\n\n"
		cmd = printfWithClear("%s", text)

	case "/status":
		text := renderStatus(m.cfg.Registry, m.cfg.Supervisors)
		cmd = printfWithClear("%s", text)

	default:
		if strings.HasPrefix(input, "/") {
			// Check if it's a skill slash command
			if m.cfg.Skills != nil {
				parts := strings.SplitN(input, " ", 2)
				cmdName := strings.TrimPrefix(parts[0], "/")
				if s, ok := m.cfg.Skills.GetSkill(cmdName); ok && s.UserInvocable {
					// 触发 skill：构造 prompt 发送给 agent
					args := ""
					if len(parts) > 1 {
						args = strings.TrimSpace(parts[1])
					}
					prompt := buildSkillPrompt(s, args)
					return false, m.startStreamFromInput(prompt)
				}
			}
			text := agentStyle.Render("Solo:") + "\n" + errorStyle.Render("✗ Unknown command: "+input+". Type /help") + "\n\n"
			cmd = printfWithClear("%s", text)
		}
	}

	if cmd == nil {
		return false, nil
	}
	if logoCmd != nil {
		return false, tea.Sequence(logoCmd, cmd)
	}
	return false, cmd
}

// buildSkillPrompt 构造触发 skill 的 prompt
func buildSkillPrompt(s *skill.Skill, args string) string {
	if args != "" {
		return "Use the '" + s.ID + "' skill with these arguments: " + args
	}
	return "Use the '" + s.ID + "' skill."
}

// startStreamFromInput 从输入启动流式生成
func (m *model) startStreamFromInput(input string) tea.Cmd {
	// 复用 TUI 已有的 startStream 逻辑
	// 这里需要创建一个新的 stream
	m.nextStreamID++
	sid := m.nextStreamID
	return m.startStream(input, sid)
}
