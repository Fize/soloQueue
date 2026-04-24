#!/bin/bash
# 1. Update text input style
sed -i '/ti.PromptStyle =/d' internal/tui/tui.go
sed -i '/ti.PlaceholderStyle =/d' internal/tui/tui.go
sed -i 's/ti.Prompt = ""/ti.Prompt = ""\n\tvar inputStyles textinput.Styles\n\tinputStyles.Text = lipgloss.NewStyle().Background(lipgloss.Color("236"))\n\tinputStyles.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Background(lipgloss.Color("236"))\n\tti.SetStyles(inputStyles)/g' internal/tui/tui.go

# 2. Key map replacements
sed -i 's/tea.KeyCtrlC/tea.KeyCtrlC/g' internal/tui/update.go
sed -i 's/tea.KeyCtrlD/tea.KeyCtrlD/g' internal/tui/update.go
sed -i 's/tea.KeyEsc/tea.KeyEscape/g' internal/tui/update.go
sed -i 's/tea.KeyEnter/tea.KeyEnter/g' internal/tui/update.go
sed -i 's/tea.KeyUp/tea.KeyUp/g' internal/tui/update.go
sed -i 's/tea.KeyDown/tea.KeyDown/g' internal/tui/update.go
sed -i 's/tea.KeyCtrlT/tea.KeyCtrlT/g' internal/tui/update.go
sed -i 's/tea.KeyCtrlO/tea.KeyCtrlO/g' internal/tui/update.go
