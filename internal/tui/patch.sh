#!/bin/bash
# 1. Update View return type
sed -i 's/func (m \*model) View() string {/func (m \*model) View() tea.View {/g' internal/tui/view.go

# 2. Update View to return tea.View
sed -i 's/return sb.String()/v := tea.NewView(sb.String())\n\tv.AltScreen = m.useAltScreen\n\treturn v/g' internal/tui/view.go
sed -i 's/return ""/return tea.NewView("")/g' internal/tui/view.go
sed -i 's/return styleError.Render("fatal: "+m.fatalErr.Error()) + "\\n"/return tea.NewView(styleError.Render("fatal: "+m.fatalErr.Error()) + "\\n")/g' internal/tui/view.go

# 3. Update update.go key parsing
sed -i 's/switch msg.Type {/switch msg.Key().Code {/g' internal/tui/update.go
sed -i 's/if msg.Type != tea.KeyCtrlC/if msg.Key().Code != tea.KeyCtrlC/g' internal/tui/update.go
sed -i 's/msg.Runes\[0\]/[]rune(msg.Key().Text)[0]/g' internal/tui/update.go
sed -i 's/len(msg.Runes) == 1/len(msg.Key().Text) == 1/g' internal/tui/update.go
sed -i 's/case tea.KeyRunes:/default:/g' internal/tui/update.go

# 4. Remove tea.WithAltScreen option in tui.go
sed -i 's/opts = append(opts, tea.WithAltScreen())//g' internal/tui/tui.go
