package tui

import "os"

// ─── Terminal environment detection ───────────────────────────────────────────

// shouldUseAltScreen 判断是否应使用 alt-screen 模式。
//
// 默认使用 inline 模式（内容直接追加到终端滚动历史，与 Claude Code 一致）。
// 如需固定输入框到底部、消除闪烁，可设置环境变量 ALT_SCREEN=1 启用全屏。
func shouldUseAltScreen() bool {
	return os.Getenv("ALT_SCREEN") != "" || os.Getenv("SOLOQUEUE_ALT_SCREEN") != ""
}
