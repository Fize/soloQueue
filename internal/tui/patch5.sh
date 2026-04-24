#!/bin/bash
# 1. Add Program pointer to model to use p.Println
sed -i '/fatalErr error/a \p *tea.Program' internal/tui/tui.go
sed -i 's/return tea.NewProgram(m, opts...)/p := tea.NewProgram(m, opts...)\n\tm.p = p\n\treturn p/g' internal/tui/tui.go

# 2. Add Println to update.go
sed -i 's/m.addScrollLine(tail, styleAI)/m.addScrollLine(tail, styleAI)\n\t\t\tm.p.Println(styleAI.Render(tail))/g' internal/tui/update.go
sed -i 's/m.addScrollLine(line, style)/m.scrollback = append(m.scrollback, scrollLine{content: line, style: style})\n\tm.lastLineEmpty = (line == "")\n\n\tif !m.useAltScreen {\n\t\tm.p.Println(style.Render(line))\n\t}\n\n\tif len(m.scrollback) > maxScrollbackLines {\n\t\tkeep := maxScrollbackLines * 9 \/ 10\n\t\tm.scrollback = m.scrollback[len(m.scrollback)-keep:]\n\t}/g' internal/tui/scrollback.go
sed -i '/func (m \*model) addScrollLine/d' internal/tui/scrollback.go
sed -i '/m.scrollback = append(m.scrollback, scrollLine{content: content, style: style})/d' internal/tui/scrollback.go
sed -i '/m.lastLineEmpty = (content == "")/d' internal/tui/scrollback.go
sed -i '/if len(m.scrollback) > maxScrollbackLines {/,+3d' internal/tui/scrollback.go
sed -i 's/\/\/ ─── Scrollback management/\/\/ ─── Scrollback management ────────────────────────────────────────────────────\n\nfunc (m \*model) addScrollLine(content string, style lipgloss.Style) {\n\tm.scrollback = append(m.scrollback, scrollLine{content: content, style: style})\n\tm.lastLineEmpty = (content == "")\n\n\tif !m.useAltScreen \&\& m.p != nil {\n\t\tm.p.Println(style.Render(content))\n\t}\n\n\tif len(m.scrollback) > maxScrollbackLines {\n\t\tkeep := maxScrollbackLines * 9 \/ 10\n\t\tm.scrollback = m.scrollback[len(m.scrollback)-keep:]\n\t}\n}/g' internal/tui/scrollback.go

