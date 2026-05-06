package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/skill"
)

// ─── Built-in commands ────────────────────────────────────

func (m *model) handleBuiltin(input string) (bool, tea.Cmd) {
	name := strings.ToLower(strings.TrimSpace(input))

	switch name {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		text := "Commands: /help /clear /status /version /quit"
		if m.cfg.Skills != nil {
			skills := m.cfg.Skills.Skills()
			var userSkills []*skill.Skill
			for _, s := range skills {
				if s.UserInvocable && !s.DisableModelInvocation {
					userSkills = append(userSkills, s)
				}
			}
			if len(userSkills) > 0 {
				text += "\nSkills:"
				for _, s := range userSkills {
					text += "\n  /" + s.ID + " — " + s.Description
				}
			}
		}
		m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
		m.messages = append(m.messages, message{role: "agent", content: text, timestamp: time.Now()})
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return false, nil

	case "/clear":
		if m.isGenerating {
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.resetGenState()
			m.current = nil
		}
		m.messages = nil
		m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
		text := "◆  context cleared"
		m.messages = append(m.messages, message{role: "agent", content: text, timestamp: time.Now()})
		m.resizeViewport()
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		// Clear session asynchronously to avoid blocking the TUI event loop.
		// session.Clear() may be slow: mutex lock, timeline append, memory hook.
		if m.sess != nil {
			return false, clearSessionCmd(m.sess)
		}
		return false, nil

	case "/version":
		text := "SoloQueue " + m.cfg.Version
		m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
		m.messages = append(m.messages, message{role: "agent", content: text, timestamp: time.Now()})
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return false, nil

	case "/status":
		text := renderStatus(m.cfg.Registry, m.cfg.SupervisorsFn)
		m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
		m.messages = append(m.messages, message{role: "agent", content: text, timestamp: time.Now()})
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return false, nil

	case "/agents":
		m.showAgents = !m.showAgents
		m.resizeViewport()
		m.rebuildViewportContent()
		return false, nil

	default:
		if strings.HasPrefix(input, "/") {
			if m.cfg.Skills != nil {
				parts := strings.SplitN(input, " ", 2)
				cmdName := strings.TrimPrefix(parts[0], "/")
				if s, ok := m.cfg.Skills.GetSkill(cmdName); ok && s.UserInvocable {
					args := ""
					if len(parts) > 1 {
						args = strings.TrimSpace(parts[1])
					}
					prompt := buildSkillPrompt(s, args)
					return false, m.startStreamFromInput(input, prompt)
				}
			}
			text := "✗ Unknown command: " + input + ". Type /help"
			m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
			m.messages = append(m.messages, message{role: "agent", content: text, timestamp: time.Now()})
			m.rebuildViewportContent()
			m.viewport.GotoBottom()
			return false, nil
		}
	}

	return false, nil
}

// buildSkillPrompt constructs the prompt for triggering a skill.
func buildSkillPrompt(s *skill.Skill, args string) string {
	if args != "" {
		return "Use the '" + s.ID + "' skill with these arguments: " + args
	}
	return "Use the '" + s.ID + "' skill."
}

// startStreamFromInput starts a stream from the given input string.
// displayInput is the original user input to show in the conversation.
func (m *model) startStreamFromInput(displayInput string, streamInput string) tea.Cmd {
	m.messages = append(m.messages, message{role: "user", content: displayInput, timestamp: time.Now()})
	m.nextStreamID++
	sid := m.nextStreamID
	m.isGenerating = true
	m.spinner = newSpinner()
	m.genStartTime = time.Now()
	m.genPhase = phaseWaiting
	m.promptTokens = 0
	m.outputTokens = 0
	m.cacheHitTokens = 0
	m.cacheMissTokens = 0
	m.reasoningTokens = 0
	m.genSummary = ""
	m.current = &streamState{
		toolExecMap: make(map[string]*toolExecInfo),
	}
	m.messages = append(m.messages, message{role: "agent", timestamp: time.Now()})
	m.resizeViewport()
	m.rebuildViewportContent()
	m.viewport.GotoBottom()
	return m.startStream(streamInput, sid)
}

// clearSessionCmd runs session.Clear() asynchronously to avoid blocking the
// TUI event loop. session.Clear() can be slow because it locks the session
// mutex, appends a control event to the timeline, and calls a memory hook.
func clearSessionCmd(sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Clear()
		return nil
	}
}
