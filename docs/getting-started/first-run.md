# First run

> 中文：[第一次运行](../zh/getting-started/first-run.md)

## 1. Start the backend

From the repository root, I start the backend with:

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve --verbose
~~~

I read the listening address printed by the server. With the default settings, I open
http://127.0.0.1:57647. On first start, I let SoloQueue create ~/.soloqueue/ and
initialize settings.yaml, agent/team storage, SQLite, and log directories as
I use them.

## 2. Verify the provider

I open Settings → Models and confirm that at least one provider and one model
is enabled. My default configuration points at DeepSeek and reads the key from
DEEPSEEK_API_KEY; I can instead edit
[settings.yaml](../reference/configuration.md) or use the settings screens.

If the model list is empty, I check the provider ID, model ID, and model_routes
entries. I use provider:model for a route value, for example
deepseek:deepseek-v4-flash-thinking.

## 3. Send a small request

I open Chat, create a new session, and ask a short question. My first request
may initialize the default profile and agent catalog. I keep the server
terminal open while testing so configuration and tool errors are visible.

## 4. Understand confirmations

When my tools match a confirmation policy, I see SoloQueue pause the run and show a
confirmation card in the UI. I review the command, path, and requested scope
before allowing it. I treat the --bypass server flag as a global confirmation bypass
globally, so I use it only for controlled experiments.

## 5. Find local state

I use ~/.soloqueue/ by default (or the directory named by SOLOQUEUE_WORK_DIR).
I read [Data, logs, and backup](../operations/data-and-backup.md) before
copying or deleting it.

## If the first run fails

- **Blank portal**: I run make build-web, then restart the server.
- **No model response**: I verify the provider key and enabled model.
- **Cannot reach backend**: I confirm the process is listening on the address
  shown in the terminal and that the desktop proxy uses the same port.
- **Remote 403**: I configure authentication before using a non-loopback host;
  I then read [Remote access](../operations/remote-access.md).
