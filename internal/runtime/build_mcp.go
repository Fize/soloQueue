package runtime

import (
	"context"
	"os"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	lspmcp "github.com/xiaobaitu/soloqueue/internal/mcp/lsp"
)

// buildMCP initializes the MCP Loader, Manager, and built-in LSP MCP.
func (bc *buildContext) buildMCP() {
	mcpConfigPath := filepath.Join(bc.workDir, "mcp.json")
	mcpLoader, mcpLoaderErr := mcp.NewLoader(mcpConfigPath, bc.log)
	if mcpLoaderErr != nil {
		bc.log.Warn(logger.CatMCP, "failed to create MCP config loader", "err", mcpLoaderErr)
	}
	var mcpMgr *mcp.Manager
	if mcpLoader != nil {
		if err := mcpLoader.Load(); err != nil {
			bc.log.Warn(logger.CatMCP, "failed to load mcp.json, creating default", "err", err.Error())
		}
		if err := mcpLoader.Watch(); err != nil {
			bc.log.Warn(logger.CatMCP, "failed to watch mcp.json for hot-reload", "err", err.Error())
		}
		mcpMgr = mcp.NewManager(mcpLoader, bc.log)
	}
	bc.mcpLoader = mcpLoader
	bc.mcpMgr = mcpMgr

	// ── LSP MCP (built-in LSP-based MCP) ─────────────────────────────────────
	rootPath, _ := os.Getwd()
	lspMgr := lspmcp.NewManager(rootPath, bc.log)
	defs := lspmcp.BuiltinServers()

	// Apply user overrides from settings if present.
	if len(bc.settings.LSPMCP.Servers) > 0 {
		userDefs := make(map[string]config.LSPMCPEntry)
		for _, s := range bc.settings.LSPMCP.Servers {
			userDefs[s.ID] = s
		}
		filtered := defs[:0]
		for _, d := range defs {
			if u, ok := userDefs[d.ID]; ok {
				if u.Disabled {
					continue
				}
				if u.Command != "" {
					d.Command = u.Command
				}
				if u.Args != nil {
					d.Args = u.Args
				}
				if u.Languages != nil {
					d.Languages = u.Languages
				}
				if u.Extensions != nil {
					d.Extensions = u.Extensions
				}
			}
			filtered = append(filtered, d)
		}
		defs = filtered
	}

	if err := lspMgr.Start(context.Background(), defs); err != nil {
		bc.log.Warn(logger.CatMCP, "failed to start LSP MCP", "err", err.Error())
	} else if mcpMgr != nil {
		mcpMgr.RegisterVirtual("builtin-lsp", lspMgr.GetTools)
	}
	bc.lspMgr = lspMgr
}
