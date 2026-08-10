# Skills, MCP, and LSP

Skills and MCP servers extend the built-in tool layer without changing the
core session model.

## Skills

The bundled catalog is copied into the embedded portal build. User skills live
under ~/.soloqueue/skills/ and can be created, imported, edited, toggled, or
updated from the Skills settings surface. A skill is a directory containing a
SKILL.md file with YAML frontmatter and Markdown instructions; supporting
scripts and references may live beside it.

Skills are hot-reloaded. Keep descriptions specific enough that the model can
choose the right skill, and avoid installing large catalogs that add prompt
overhead to every request. The CLI report helps find never-invoked skills and
weak descriptions.

~~~bash
soloqueue skills report --days 30
~~~

## MCP servers

External MCP definitions are stored in ~/.soloqueue/mcp.json. SoloQueue accepts
the standard mcpServers map shape:

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

Use Settings → Capabilities to inspect available tools and MCP policy state.
MCP processes run with the permissions of the SoloQueue process, so review
commands, environment variables, and paths before enabling a server.

## LSP-backed tools

LSP MCP entries are configured under lspmcp in settings.yaml. Each entry names
the executable, arguments, languages, extensions, and enabled state. The
language server must be installed and reachable from the server process PATH.

## Reload and diagnose

After editing a skill or MCP file, wait for the hot-reload log and refresh the
Capabilities screen. If a server is missing, check its command, working
directory, executable permissions, and the server log before reinstalling
anything.
