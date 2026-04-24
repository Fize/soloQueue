#!/bin/bash
sed -i 's/inputStyles.Text/inputStyles.Focused.Text/g' internal/tui/tui.go
sed -i 's/inputStyles.Placeholder/inputStyles.Focused.Placeholder/g' internal/tui/tui.go
