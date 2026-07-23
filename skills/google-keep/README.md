# Google Keep Skill - Token Guide and Usage Documentation

This built-in `google-keep` skill for soloQueue uses `gkeepapi` to read, create, edit, search, and delete Google Keep notes and checklists.

---

## 🔑 How to Get Your Google Keep Token

Because Google **does not offer an official public API for personal accounts**, authentication relies on obtaining an OAuth master token from your Google account. Once stored locally, this token **does not expire**.

### Step 1: Obtain `oauth_token` via Browser

1. Open the official Google Embedded Setup URL in your browser:
   👉 **[https://accounts.google.com/EmbeddedSetup](https://accounts.google.com/EmbeddedSetup)**
2. Log in with your Google account (your `GOOGLE_KEEP_EMAIL`).
3. Click **"I agree"**.
   *(Note: The page might spin continuously after clicking — this is expected, you can ignore it).*
4. Press `F12` to open Developer Tools (DevTools):
   * Go to the **Application** tab.
   * Expand **Cookies** on the left menu and select `https://accounts.google.com`.
   * Find the cookie named **`oauth_token`** and copy its value (starts with `oauth2_4/`).

---

### Step 2: Configure System Environment Variables

Add the credentials to your system environment variables (e.g., in your `~/.zshrc` or `~/.bashrc`):

#### Option A: Set `GOOGLE_KEEP_OAUTH_TOKEN` (Recommended — auto-exchanges on first run)

```bash
export GOOGLE_KEEP_EMAIL="your_email@gmail.com"
export GOOGLE_KEEP_OAUTH_TOKEN="oauth2_4/your_oauth_token_here"
```

*Upon first execution, `gkeep.py` automatically exchanges `oauth_token` for a persistent `master_token` and caches it locally in the skill directory.*

#### Option B: Set `GOOGLE_KEEP_MASTER_TOKEN` Directly

If you already have a `master_token` (typically starts with `aas_et/...`):

```bash
export GOOGLE_KEEP_EMAIL="your_email@gmail.com"
export GOOGLE_KEEP_MASTER_TOKEN="aas_et/your_master_token_here"
```

Apply the environment variables:
```bash
source ~/.zshrc   # or source ~/.bashrc
```

---

## 🚀 CLI Usage & Debugging

Once system environment variables are configured, run `gkeep.py`:

```bash
# View help
python3 skills/google-keep/gkeep.py --help

# List notes
python3 skills/google-keep/gkeep.py list --json

# Search notes
python3 skills/google-keep/gkeep.py search "groceries"

# Create a standard note
python3 skills/google-keep/gkeep.py create --title "Meeting Notes" --text "Discuss Q3 roadmap"

# Create a checklist
python3 skills/google-keep/gkeep.py create --title "Shopping List" --list --items "Milk" "Eggs" "Bread"

# Add item to a checklist
python3 skills/google-keep/gkeep.py add-item "Shopping List" "Apples"

# Delete a note
python3 skills/google-keep/gkeep.py delete "Meeting Notes"
```

---

## 🛠️ Automatic Dependency Setup

`gkeep.py` includes an auto-bootstrap mechanism. On first run, it automatically creates a local virtual environment in `skills/google-keep/.venv` and installs `gkeepapi` and `gpsoauth`. No manual `pip install` or Python environment setup is required.
