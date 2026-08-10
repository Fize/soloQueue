# Skills, MCP, and LSP

> 中文：[Skills、MCP 与 LSP](../zh/guides/skills-and-mcp.md)

I use Skills and MCP servers to extend the built-in tool layer without changing
my core session model.

## Skills

I copy the bundled catalog into the embedded portal build. I keep user skills
under ~/.soloqueue/skills/ and create, import, edit, toggle, or update them
from the Skills settings surface. I define a skill as a directory containing a
SKILL.md file with YAML frontmatter and Markdown instructions; I can keep
supporting scripts and references beside it.

I hot-reload Skills. I keep descriptions specific enough that the model can
choose the right skill, and I avoid installing large catalogs that add prompt
overhead to every request. I use the CLI report to find never-invoked skills
and weak descriptions.

~~~bash
soloqueue skills report --days 30
~~~

## MCP servers

I store external MCP definitions in ~/.soloqueue/mcp.json and use the standard
mcpServers map shape:

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

I use Settings → Capabilities to inspect available tools and MCP policy state.
I run MCP processes with my SoloQueue process permissions, so I review commands,
environment variables, and paths before enabling a server.

## LSP-backed tools

I configure LSP MCP entries under lspmcp in settings.yaml. I define each entry
with the executable, arguments, languages, extensions, and enabled state. I install the
language server and make it reachable from the server process PATH.

## Reload and diagnose

After editing a skill or MCP file, I wait for the hot-reload log and refresh the
Capabilities screen. If a server is missing, I check its command, working
directory, executable permissions, and server log before reinstalling anything.
