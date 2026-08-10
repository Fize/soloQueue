# Security and permissions

SoloQueue is a personal, self-hosted application. Its controls reduce
accidental or unexpected actions, but they are not a complete security
boundary.

## Tool confirmations

The agent can pause before a tool action and ask for confirmation. Review the
command, path, network target, and expected side effect before allowing it.
Confirmation is an interlock in the agent loop; it is not a substitute for OS
permissions, container isolation, or code review.

The server flag below bypasses all tool confirmations:

~~~bash
./soloqueue serve --bypass
~~~

Use it only for a controlled local experiment. Do not combine it with a
publicly reachable listener or an unreviewed agent template.

## Filesystem and project scope

Register projects with explicit absolute paths. A project path determines where
tools and delegated work are allowed to operate, but the underlying process
still runs with the operating system user's permissions. Keep the runtime and
project directories separate and use a dedicated OS account for higher-risk
experiments.

## Network and subprocesses

HTTP fetches, web search, MCP servers, language servers, and shell commands can
reach resources available to the server process. Review shell confirmation and
block patterns, allowed HTTP hosts, MCP commands, and environment variables.
The default private-network HTTP block is a policy setting, not proof that
every tool or subprocess is isolated.

## Secrets

Provider and channel credentials may be stored in settings.yaml or local
database state. Use environment-variable references where supported, keep the
work directory private, and do not commit it. Logs and memory can also contain
prompts, paths, tool arguments, and responses.

## Remote use

The default loopback listener is the safest operating mode. Remote access
requires explicit authentication and a trusted network boundary; see
[Remote access](remote-access.md). The localhost bypass and unauthenticated
health check are intentional implementation details, not a deployment
recommendation.
