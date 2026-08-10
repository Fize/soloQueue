# First run

## 1. Start the backend

From the repository root:

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve --verbose
~~~

The server prints its listening address. With the default settings, open
http://127.0.0.1:57647. The application creates ~/.soloqueue/ on first start
and initializes settings.yaml, agent/team storage, SQLite, and log
directories as they are needed.

## 2. Verify the provider

Open Settings → Models and confirm that at least one provider and one model
are enabled. The default configuration points at DeepSeek and reads the key
from DEEPSEEK_API_KEY; you can instead edit
[settings.yaml](../reference/configuration.md) or use the settings screens.

If the model list is empty, check the provider ID, model ID, and model_routes
entries. A route value uses provider:model, for example
deepseek:deepseek-v4-flash-thinking.

## 3. Send a small request

Open Chat, create a new session, and ask a short question. The first request
may initialize the default profile and agent catalog. Keep the server terminal
open while testing so configuration and tool errors are visible.

## 4. Understand confirmations

Tools that match a confirmation policy pause the run and show a confirmation
card in the UI. Review the command, path, and requested scope before allowing
it. The --bypass server flag disables these confirmations globally and is
intended only for controlled experiments.

## 5. Find local state

The default work directory is ~/.soloqueue/ (or the directory named by
SOLOQUEUE_WORK_DIR). See [Data, logs, and backup](../operations/data-and-backup.md)
before copying or deleting it.

## If the first run fails

- **Blank portal**: run make build-web, then restart the server.
- **No model response**: verify the provider key and enabled model.
- **Cannot reach backend**: confirm the process is listening on the address
  shown in the terminal and that the desktop proxy uses the same port.
- **Remote 403**: configure authentication before using a non-loopback host;
  see [Remote access](../operations/remote-access.md).
