package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// newTestTextarea creates a minimal textarea for testing purposes.
func newTestTextarea(value string, width int) textarea.Model {
	ta := textarea.New()
	ta.SetValue(value)
	ta.SetWidth(width)
	return ta
}

// newTestModel creates a fully initialized model for testing.
func newTestModel() model {
	ta := textarea.New()
	ta.SetValue("")
	ta.SetWidth(80)
	ta.DynamicHeight = true
	ta.MaxHeight = maxComposerLines
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true

	return model{
		cfg:        Config{Version: "v0.1.0", ModelID: "test-model"},
		spinner:    newSpinner(),
		sidebar:    newSidebar(nil, nil, nil, nil, ""),
		textArea:   ta,
		viewport:   vp,
		width:      80,
		height:     24,
		messages:   []message{},
		history:    []string{},
		ctx:        context.Background(),
		focus:      focusComposer,
		showAgents: true,
	}
}

// keyPress creates a KeyPressMsg that String() returns the given name.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	case "ctrl+y":
		return tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		return tea.KeyPressMsg{Text: name}
	}
}

// ─── fmt helper ───────────────────────────────────────────────────────────

var _ = fmt.Sprintf
var _ = context.Background
var _ = time.Second
