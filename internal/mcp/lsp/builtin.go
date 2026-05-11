package lsp

// ServerDef defines an LSP server configuration.
type ServerDef struct {
	ID        string   // Unique identifier (e.g., "gopls")
	Command   string   // Executable name
	Args      []string // CLI arguments
	Languages []string // Language IDs for LSP initialize
	Extensions []string // File extensions this server handles
}

// BuiltinServers returns the hardcoded built-in LSP server definitions.
func BuiltinServers() []ServerDef {
	return []ServerDef{
		{
			ID:        "gopls",
			Command:   "gopls",
			Args:      nil,
			Languages: []string{"go"},
			Extensions: []string{".go"},
		},
		{
			ID:        "bash",
			Command:   "bash-language-server",
			Args:      []string{"start"},
			Languages: []string{"bash"},
			Extensions: []string{".sh", ".bash", ".zsh", ".ksh"},
		},
		{
			ID:        "pyright",
			Command:   "pyright-langserver",
			Args:      []string{"--stdio"},
			Languages: []string{"python"},
			Extensions: []string{".py", ".pyi"},
		},
		{
			ID:        "typescript",
			Command:   "typescript-language-server",
			Args:      []string{"--stdio"},
			Languages: []string{"typescript", "javascript", "typescriptreact", "javascriptreact"},
			Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"},
		},
		{
			ID:        "vue",
			Command:   "vue-language-server",
			Args:      []string{"--stdio"},
			Languages: []string{"vue"},
			Extensions: []string{".vue"},
		},
		{
			ID:        "yaml",
			Command:   "yaml-language-server",
			Args:      []string{"--stdio"},
			Languages: []string{"yaml"},
			Extensions: []string{".yaml", ".yml"},
		},
		{
			ID:        "lua",
			Command:   "lua-language-server",
			Args:      nil,
			Languages: []string{"lua"},
			Extensions: []string{".lua"},
		},
		{
			ID:        "clangd",
			Command:   "clangd",
			Args:      nil,
			Languages: []string{"c", "cpp"},
			Extensions: []string{".c", ".h", ".cpp", ".hpp", ".cc", ".cxx", ".hxx", ".c++", ".h++"},
		},

	}
}
