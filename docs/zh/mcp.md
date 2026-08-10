# MCP 与 LSP

[English: MCP and LSP](../mcp.md)

我把 MCP 配置和策略放在 internal/agenttools/mcp，面向用户的设置方法见
[Skills、MCP 与 LSP](guides/skills-and-mcp.md)。

我使用 ~/.soloqueue/mcp.json 中的标准 mcpServers Map、stdio Server 进程、
策略状态，以及 settings.yaml 中 lspmcp 配置的内置 LSP 工具。

我让 MCP 和 Language Server 继承 SoloQueue 服务端可用的权限和环境，并在启用它们
之前会检查命令、参数、环境变量、网络访问和项目范围。
