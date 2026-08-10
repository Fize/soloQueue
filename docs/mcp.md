# MCP and LSP

> 中文：[MCP 与 LSP](zh/mcp.md)

I keep MCP configuration and policy under internal/agenttools/mcp. I document
the user-facing setup in
[Skills, MCP, and LSP](guides/skills-and-mcp.md).

I use the standard mcpServers map in ~/.soloqueue/mcp.json, stdio server
processes, policy state, and built-in LSP-backed tools configured under lspmcp
in settings.yaml.

I run MCP and language-server processes with the permissions and environment
available to my SoloQueue server. I review commands, arguments, environment
variables, network access, and project scope before enabling them.
