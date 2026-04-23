package tui

import "os"

// ─── Terminal environment detection ───────────────────────────────────────────

type terminalEnv struct {
	Program  string // iterm2, vscode, tmux, kitty, unknown...
	IsTmux   bool
	IsVSCode bool
	IsSSH    bool
}

func detectTerminal() terminalEnv {
	env := terminalEnv{}
	tp := os.Getenv("TERM_PROGRAM")

	env.IsTmux = os.Getenv("TMUX") != ""
	env.IsVSCode = tp == "vscode" || tp == "codebuddy" || tp == "cursor" ||
		os.Getenv("VSCODE_INJECTION") != ""
	env.IsSSH = os.Getenv("SSH_TTY") != "" ||
		os.Getenv("SSH_CONNECTION") != "" ||
		os.Getenv("SSH_CLIENT") != ""

	switch tp {
	case "iTerm.app":
		env.Program = "iterm2"
	case "vscode":
		env.Program = "vscode"
	case "codebuddy":
		env.Program = "codebuddy"
	case "cursor":
		env.Program = "cursor"
	case "ghostty":
		env.Program = "ghostty"
	case "WezTerm":
		env.Program = "wezterm"
	default:
		if env.IsTmux {
			env.Program = "tmux"
		} else if os.Getenv("KITTY_WINDOW_ID") != "" {
			env.Program = "kitty"
		} else if os.Getenv("ALACRITTY_WINDOW_ID") != "" {
			env.Program = "alacritty"
		} else {
			env.Program = "unknown"
		}
	}

	return env
}

// shouldUseAltScreen 判断是否应使用 alt-screen 模式。
//
// Alt-screen 模式下程序独占终端屏幕，输入框可精确定位到底部。
// 在 tmux 和 SSH 环境下降级到 inline 模式，避免鼠标捕获冲突和兼容性问题。
// 用户可通过 NO_ALT_SCREEN=1 环境变量强制禁用 alt-screen。
func shouldUseAltScreen() bool {
	if os.Getenv("NO_ALT_SCREEN") != "" {
		return false
	}
	env := detectTerminal()
	if env.IsTmux || env.IsSSH {
		return false
	}
	return true
}
