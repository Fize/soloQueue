# Security and permissions

> 中文：[安全与权限](../zh/operations/security-and-permissions.md)

I run SoloQueue as a personal, self-hosted application. I use its controls to
reduce accidental or unexpected actions, but I do not treat them as a complete
security boundary.

## Tool confirmations

My agent can pause before a tool action and ask for confirmation. I review the
command, path, network target, and expected side effect before allowing it.
I treat confirmation as an interlock in the agent loop, not a substitute for OS
permissions, container isolation, or code review.

I can bypass all tool confirmations with the server flag below:

~~~bash
./soloqueue serve --bypass
~~~

I use it only for a controlled local experiment. I do not combine it with a
publicly reachable listener or an unreviewed agent template.

## Filesystem and project scope

I register projects with explicit absolute paths. I use a project path to
determine where tools and delegated work are allowed to operate, but the underlying
process still runs with the operating system user's permissions. I keep the
runtime and project directories separate and use a dedicated OS account for
higher-risk experiments.

## Network and subprocesses

I let my HTTP fetches, web search, MCP servers, language servers, and shell
commands reach resources available to the server process. I review shell
confirmation and block patterns, allowed HTTP hosts, MCP commands, and
environment variables. I treat the default private-network HTTP block as a
policy setting, not proof that every tool or subprocess is isolated.

## Secrets

My provider and channel credentials may be stored in settings.yaml or local
database state. I use environment-variable references where supported, keep
the work directory private, and do not commit it. My logs and memory can also
contain prompts, paths, tool arguments, and responses.

## Remote use

I use the default loopback listener as my safest operating mode. Remote access
requires explicit authentication and a trusted network boundary; I document it in
[Remote access](remote-access.md). The localhost bypass and unauthenticated
health check are intentional implementation details, not a deployment
recommendation.
