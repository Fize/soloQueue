package main

// A simple program demonstrating the text area component from the Bubbles
// component library.

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Oof: %v\n", err)
	}
}

type model struct {
	viewport    viewport.Model
	messages    []string
	textarea    textarea.Model
	senderStyle lipgloss.Style
	err         error
}

func initialModel() model {
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.SetVirtualCursor(false)
	ta.Focus()

	ta.Prompt = "┃ "
	ta.CharLimit = 280

	ta.SetWidth(30)
	ta.SetHeight(1)

	// Remove cursor line styling
	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(s)

	ta.ShowLineNumbers = false

	vp := viewport.New(viewport.WithWidth(30), viewport.WithHeight(5))
	vp.SetContent(`Welcome to the chat room!
Type a message and press Enter to send.`)
	vp.KeyMap.Left.SetEnabled(false)
	vp.KeyMap.Right.SetEnabled(false)

	ta.KeyMap.InsertNewline.SetEnabled(false)

	return model{
		textarea:    ta,
		messages:    []string{},
		viewport:    vp,
		senderStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		err:         nil,
	}
}

func visualLineCount(ta textarea.Model) int {
	lines := strings.Split(ta.Value(), "\n")
	promptWidth := lipgloss.Width(ta.Prompt)
	contentWidth := ta.Width() - promptWidth
	if contentWidth <= 0 {
		contentWidth = 1
	}
	count := 0
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			count++
		} else {
			count += (lineWidth + contentWidth - 1) / contentWidth
		}
	}
	return max(1, count)
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		// m.viewport.SetHeight(msg.Height - m.textarea.Height() - 2)
		h := msg.Height - m.textarea.Height() - 50
		if h >= 0 {
			m.viewport.SetHeight(h)
		} else {
			m.viewport.SetHeight(msg.Height - m.textarea.Height())
		}

		if len(m.messages) > 0 {
			// Wrap content before setting it.
			m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width()).Render(strings.Join(m.messages, "\n")))
		}
		m.viewport.GotoBottom()
	case tea.PasteMsg:
		var cmd tea.Cmd
		oldHeight := m.textarea.Height()
		m.textarea, cmd = m.textarea.Update(msg)
		newHeight := visualLineCount(m.textarea)
		m.textarea.SetHeight(newHeight)
		delta := newHeight - oldHeight
		m.viewport.SetHeight(m.viewport.Height() - delta)
		return m, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			fmt.Println(m.textarea.Value())
			return m, tea.Quit
		case "enter":
			m.messages = append(m.messages, m.senderStyle.Render("You: ")+m.textarea.Value())
			m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width()).Render(strings.Join(m.messages, "\n")))
			oldHeight := m.textarea.Height()
			m.textarea.Reset()
			m.textarea.SetHeight(1)
			m.viewport.SetHeight(m.viewport.Height() + (oldHeight - 1))
			m.viewport.GotoBottom()
			return m, nil
		default:
			// Send all other keypresses to the textarea.
			var cmd tea.Cmd
			oldHeight := m.textarea.Height()
			m.textarea, cmd = m.textarea.Update(msg)
			newHeight := visualLineCount(m.textarea)
			m.textarea.SetHeight(newHeight)
			delta := newHeight - oldHeight
			m.viewport.SetHeight(m.viewport.Height() - delta)
			return m, cmd
		}

	case cursor.BlinkMsg:
		// Textarea should also process cursor blinks.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) View() tea.View {
	viewportView := m.viewport.View()
	v := tea.NewView(viewportView + "\n" + m.textarea.View())
	c := m.textarea.Cursor()
	if c != nil {
		c.Y += lipgloss.Height(viewportView)
	}
	v.Cursor = c
	v.AltScreen = false
	return v
}
