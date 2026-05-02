package tui

const (
	compactBreakpoint = 90
	minMainWidth      = 24
	minBodyHeight     = 4
	maxComposerLines  = 8
)

type layoutMode int

const (
	layoutCompact layoutMode = iota
	layoutTwoPane
)

type layout struct {
	mode layoutMode

	width  int
	height int

	headerH   int
	bodyH     int
	composerH int
	footerH   int

	leftW int
	mainW int

	composerW        int
	composerLines    int
	maxComposerLines int
}

func (m *model) computeLayout() layout {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	mode := layoutCompact
	if width >= compactBreakpoint {
		mode = layoutTwoPane
	}

	maxLines := maxComposerLinesForHeight(height)
	// Determine actual composer width based on layout mode
	composerW := max(width-4, minMainWidth)
	if width >= compactBreakpoint {
		// Two-pane: composer occupies the right pane only
		leftW := 0
		if m.showAgents {
			leftW = clampInt(width*22/100, 26, 34) + 1 // sidebar + separator
		}
		composerW = max(width-leftW-4, minMainWidth)
	}
	// Use textarea's DynamicHeight when available, otherwise estimate from content.
	composerLines := m.textArea.Height()
	if composerLines < 1 {
		composerLines = composerLineCountForValue(m.textArea.Value(), composerW, maxLines)
	}
	composerH := composerLines + 2 // separator/title + textarea lines

	ly := layout{
		mode:             mode,
		width:            width,
		height:           height,
		headerH:          1,
		composerH:        composerH,
		footerH:          1,
		composerW:        composerW,
		composerLines:    composerLines,
		maxComposerLines: maxLines,
	}

	ly.bodyH = height - ly.headerH - ly.composerH - ly.footerH
	if ly.bodyH < minBodyHeight {
		ly.bodyH = minBodyHeight
	}

	switch mode {
	case layoutTwoPane:
		if m.showAgents {
			ly.leftW = clampInt(width*22/100, 26, 34)
			ly.mainW = width - ly.leftW - 1
		} else {
			ly.mainW = width
		}
	default:
		ly.mainW = width
	}
	if ly.mainW < minMainWidth {
		ly.mainW = minMainWidth
	}

	return ly
}

func maxComposerLinesForHeight(height int) int {
	if height <= 0 {
		return 3
	}
	maxLines := height / 3
	if maxLines < 1 {
		return 1
	}
	if maxLines > maxComposerLines {
		return maxComposerLines
	}
	return maxLines
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
