package tui

const (
	minMainWidth     = 24
	minBodyHeight    = 4
	maxComposerLines = 8
)

type layout struct {
	width  int
	height int

	headerH   int
	bodyH     int
	composerH int
	footerH   int

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

	maxLines := maxComposerLinesForHeight(height)
	composerW := max(width-4, minMainWidth)

	composerLines := m.textArea.Height()
	if composerLines < 1 {
		composerLines = composerLineCountForValue(m.textArea.Value(), composerW, maxLines)
	}
	composerH := composerLines

	ly := layout{
		width:            width,
		height:           height,
		headerH:          1,
		composerH:        composerH,
		footerH:          1,
		mainW:            width,
		composerW:        composerW,
		composerLines:    composerLines,
		maxComposerLines: maxLines,
	}

	ly.bodyH = height - ly.headerH - ly.composerH - ly.footerH
	if ly.bodyH < minBodyHeight {
		ly.bodyH = minBodyHeight
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
