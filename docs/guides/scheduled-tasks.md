# Scheduled tasks

The Cron page creates recurring or one-off tasks that run through the same
session, team, model routing, and tool policies as interactive requests.

## Create a task

1. Open Scheduled tasks.
2. Choose the task type, agent/team, prompt, and schedule.
3. Add a project path when the task needs repository context.
4. Save the task and check its next-run time.
5. Use the history view to inspect status and output.

Start with a read-only task such as a report or health check. Add write or
delivery actions only after the scheduled run has been observed manually.

## Channel notifications

Cron results can be delivered through the active QQ or WeChat bridge when the
agent has a matching notify channel configured. The bridge must have a recent
user interaction after a restart; otherwise there may be no active sender.

Notification delivery is best-effort. A missing, expired, or rate-limited
channel can drop a message, so use the Web UI and cron history as the
authoritative record.

WeChat iLink delivery is more variable than QQ. See
[Channels](channels.md) for setup and protocol-specific limitations.

## Troubleshooting a missed run

- Confirm the server process stayed running at the scheduled time.
- Check the task's enabled state, schedule, and resolved model.
- Check cron history and the server logs.
- Verify the agent template's notify_channel and recent bridge activity.
- Re-run the prompt interactively before changing the schedule.
