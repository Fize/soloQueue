package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ServerDef defines an LSP server configuration.
type ServerDef struct {
	ID          string   // Unique identifier (e.g., "gopls")
	Command     string   // Executable name (used as fallback for PATH lookup)
	Args        []string // CLI arguments
	Languages   []string // Language IDs for LSP initialize
	Extensions  []string // File extensions this server handles
	InstallHint string   // User-facing install instruction shown when binary is missing
	// Resolve returns the resolved binary path for this server in the given workspace.
	// Return "" to signal the server is not available (binary not found).
	// If nil, exec.LookPath(Command) is used as the default.
	Resolve func(workspacePath string) string
}

// resolveCommand returns the binary path for def in the given workspace.
// Returns "" if the binary cannot be located.
func resolveCommand(def ServerDef, workspacePath string) string {
	if def.Resolve != nil {
		return def.Resolve(workspacePath)
	}
	path, err := exec.LookPath(def.Command)
	if err != nil {
		return ""
	}
	return path
}

// BuiltinServers returns the hardcoded built-in LSP server definitions.
func BuiltinServers() []ServerDef {
	return []ServerDef{
		{
			ID:          "gopls",
			Command:     "gopls",
			Args:        nil,
			Languages:   []string{"go"},
			Extensions:  []string{".go"},
			InstallHint: "go install golang.org/x/tools/gopls@latest",
		},
		{
			ID:          "bash",
			Command:     "bash-language-server",
			Args:        []string{"start"},
			Languages:   []string{"bash"},
			Extensions:  []string{".sh", ".bash", ".zsh", ".ksh"},
			InstallHint: "npm install -g bash-language-server",
		},
		{
			ID:          "pyright",
			Command:     "pyright-langserver",
			Args:        []string{"--stdio"},
			Languages:   []string{"python"},
			Extensions:  []string{".py", ".pyi"},
			InstallHint: "pip install pyright  (or activate a virtualenv that has pyright installed)",
			Resolve:     resolvePyright,
		},
		{
			ID:         "typescript",
			Command:    "typescript-language-server",
			Args:       []string{"--stdio"},
			Languages:  []string{"typescript", "javascript", "typescriptreact", "javascriptreact"},
			Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
			InstallHint: "npm install -g typescript-language-server typescript",
			Resolve:     resolveTypescript,
		},
		{
			ID:          "vue",
			Command:     "vue-language-server",
			Args:        []string{"--stdio"},
			Languages:   []string{"vue"},
			Extensions:  []string{".vue"},
			InstallHint: "npm install -g @vue/language-server",
			Resolve:     resolveVue,
		},
		{
			ID:          "yaml",
			Command:     "yaml-language-server",
			Args:        []string{"--stdio"},
			Languages:   []string{"yaml"},
			Extensions:  []string{".yaml", ".yml"},
			InstallHint: "npm install -g yaml-language-server",
		},
		{
			ID:          "lua",
			Command:     "lua-language-server",
			Args:        nil,
			Languages:   []string{"lua"},
			Extensions:  []string{".lua"},
			InstallHint: "https://github.com/LuaLS/lua-language-server/releases",
		},
		{
			ID:          "clangd",
			Command:     "clangd",
			Args:        nil,
			Languages:   []string{"c", "cpp"},
			Extensions:  []string{".c", ".h", ".cpp", ".hpp", ".cc", ".cxx", ".hxx", ".c++", ".h++"},
			InstallHint: "brew install llvm  (macOS) | apt install clangd  (Linux)",
		},
	}
}

// resolvePyright locates the pyright-langserver binary.
// It first checks activated virtual environments, then common venv directories
// inside the workspace, and finally falls back to the system PATH.
func resolvePyright(workspacePath string) string {
	bin := "pyright-langserver"
	binDir := "bin"
	if runtime.GOOS == "windows" {
		bin = "pyright-langserver.exe"
		binDir = "Scripts"
	}

	// 1. Activated venv via VIRTUAL_ENV.
	if venv := os.Getenv("VIRTUAL_ENV"); venv != "" {
		if candidate := filepath.Join(venv, binDir, bin); fileExists(candidate) {
			return candidate
		}
	}

	// 2. Common venv directories inside the workspace.
	for _, dir := range []string{".venv", "venv", ".env"} {
		if candidate := filepath.Join(workspacePath, dir, binDir, bin); fileExists(candidate) {
			return candidate
		}
	}

	// 3. System PATH.
	if path, err := exec.LookPath(bin); err == nil {
		return path
	}
	return ""
}

// resolveTypescript locates the typescript-language-server binary.
// It first checks the workspace's local node_modules/.bin, then falls back to PATH.
func resolveTypescript(workspacePath string) string {
	bin := "typescript-language-server"
	if runtime.GOOS == "windows" {
		bin = "typescript-language-server.cmd"
	}

	// 1. Local node_modules/.bin (project-local install).
	if local := filepath.Join(workspacePath, "node_modules", ".bin", bin); fileExists(local) {
		return local
	}

	// 2. System PATH.
	if path, err := exec.LookPath(bin); err == nil {
		return path
	}
	return ""
}

// resolveVue locates the vue-language-server binary.
// It checks the workspace's local node_modules/.bin first, then falls back to PATH.
// @vue/language-server installs its binary as "vue-language-server" in node_modules/.bin.
func resolveVue(workspacePath string) string {
	bin := "vue-language-server"
	if runtime.GOOS == "windows" {
		bin = "vue-language-server.cmd"
	}

	// 1. Local node_modules/.bin (project-local install via @vue/language-server).
	if local := filepath.Join(workspacePath, "node_modules", ".bin", bin); fileExists(local) {
		return local
	}

	// 2. System PATH.
	if path, err := exec.LookPath(bin); err == nil {
		return path
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
