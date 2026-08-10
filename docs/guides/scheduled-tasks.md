# Scheduled tasks

> 中文：[定时任务](../zh/guides/scheduled-tasks.md)

I use the Cron page to create recurring or one-off tasks. They run through the
same session, team, model routing, and tool policies as my interactive
requests.

## Create a task

1. I open Scheduled tasks.
2. I choose the task type, agent/team, prompt, and schedule.
3. I add a project path when the task needs repository context.
4. I save the task and check its next-run time.
5. I use the history view to inspect status and output.

I start with a read-only task such as a report or health check. I add write or
delivery actions only after I have observed the scheduled run manually.

## Channel notifications

I can deliver Cron results through the active QQ or WeChat bridge when I
configure a matching notify channel for the agent. After a restart, I need a
recent user interaction to establish an active sender.

I treat notification delivery as best-effort. A missing, expired, or
rate-limited channel can drop a message, so I use the Web UI and cron history
as the authoritative record.

I find WeChat iLink delivery more variable than QQ. I use [Channels](channels.md)
for setup and protocol-specific limitations.

## Troubleshooting a missed run

- I confirm the server process stayed running at the scheduled time.
- I check the task's enabled state, schedule, and resolved model.
- I check cron history and the server logs.
- I verify the agent template's notify_channel and recent bridge activity.
- I re-run the prompt interactively before changing the schedule.
