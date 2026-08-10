# MCP and LSP

MCP configuration and policy are implemented under
internal/agenttools/mcp. The user-facing setup is documented in
[Skills, MCP, and LSP](guides/skills-and-mcp.md).

The runtime supports the standard mcpServers map in ~/.soloqueue/mcp.json,
stdio server processes, policy state, and built-in LSP-backed tools configured
under lspmcp in settings.yaml.

MCP and language-server processes inherit the permissions and environment
available to the SoloQueue server. Review commands, arguments, environment
variables, network access, and project scope before enabling them.
