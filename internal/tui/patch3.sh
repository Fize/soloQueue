#!/bin/bash
# Re-do the update.go key parsing for v2
sed -i 's/switch msg.Key().Code {/switch {/g' internal/tui/update.go
sed -i 's/case tea.KeyCtrlC:/case msg.Key().Code == '\''c'\'' \&\& (msg.Key().Mod \& tea.ModCtrl != 0):/g' internal/tui/update.go
sed -i 's/case tea.KeyCtrlD:/case msg.Key().Code == '\''d'\'' \&\& (msg.Key().Mod \& tea.ModCtrl != 0):/g' internal/tui/update.go
sed -i 's/case tea.KeyCtrlT:/case msg.Key().Code == '\''t'\'' \&\& (msg.Key().Mod \& tea.ModCtrl != 0):/g' internal/tui/update.go
sed -i 's/case tea.KeyCtrlO:/case msg.Key().Code == '\''o'\'' \&\& (msg.Key().Mod \& tea.ModCtrl != 0):/g' internal/tui/update.go
sed -i 's/case tea.KeyEscape:/case msg.Key().Code == tea.KeyEsc:/g' internal/tui/update.go
sed -i 's/case tea.KeyEnter:/case msg.Key().Code == tea.KeyEnter || msg.Key().Code == tea.KeyReturn:/g' internal/tui/update.go
sed -i 's/case tea.KeyUp:/case msg.Key().Code == tea.KeyUp:/g' internal/tui/update.go
sed -i 's/case tea.KeyDown:/case msg.Key().Code == tea.KeyDown:/g' internal/tui/update.go
sed -i 's/if msg.Key().Code != tea.KeyCtrlC/if !(msg.Key().Code == '\''c'\'' \&\& (msg.Key().Mod \& tea.ModCtrl != 0))/g' internal/tui/update.go
