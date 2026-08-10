# Skills、MCP 与 LSP

[English: Skills, MCP, and LSP](../../guides/skills-and-mcp.md)

我使用 Skills 和 MCP Server 扩展内置工具层，同时保持核心会话模型不变。

## Skills

我在构建 Portal 时把内置 Catalog 复制进嵌入资源。我的用户 Skills 位于
~/.soloqueue/skills/，我可以在 Skills 设置页面创建、导入、编辑、启用或更新。
我把带 YAML frontmatter 和 Markdown 指令的 SKILL.md 放在 Skill 目录中，也可以
在同一目录中放置脚本和参考资料。

我使用 Skills 的热加载能力。我会保持描述具体，避免安装过大的 Catalog，因为每个请求
都会增加 Prompt 开销。CLI 报告可以帮助我查找未调用的 Skill 和质量较弱的描述。

~~~bash
soloqueue skills report --days 30
~~~

## MCP Server

我把外部 MCP 定义放在 ~/.soloqueue/mcp.json，并使用标准 mcpServers Map：

~~~json
{
  "mcpServers": {
    "example": {
      "command": "example-mcp",
      "args": ["--stdio"],
      "transport": "stdio",
      "enabled": true
    }
  }
}
~~~

我在 Settings → Capabilities 查看可用工具和 MCP 策略。我让 MCP 进程继承
SoloQueue 服务端的权限，因此启用前我会检查命令、环境变量和路径。

## LSP 工具

我把 LSP MCP 条目放在 settings.yaml 的 lspmcp 下，为每个条目定义可执行文件、
参数、语言、扩展名和启用状态。我会确保语言服务器已安装，并能被服务端进程从 PATH 找到。

## 重载与诊断

编辑 Skill 或 MCP 文件后，我会等待服务日志中的热加载消息，再刷新 Capabilities。
Server 缺失时，我会先检查命令、工作目录、可执行权限和服务日志。
