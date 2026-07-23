---
name: google-keep
description: |
  Google Keep CLI integration with environment-variable authentication. Manage notes, lists, and checklists with full CRUD operations.
  Use this skill when users want to read, create, search, edit, or delete notes and checklists in Google Keep.
compatibility: Requires python3 and valid GOOGLE_KEEP_EMAIL and GOOGLE_KEEP_MASTER_TOKEN (or GOOGLE_KEEP_OAUTH_TOKEN) environment variables.
required-env:
  - GOOGLE_KEEP_EMAIL
  - GOOGLE_KEEP_MASTER_TOKEN
metadata:
  author: soloqueue
  version: "1.0"
  clawdbot:
    requires:
      env:
        - GOOGLE_KEEP_EMAIL
        - GOOGLE_KEEP_MASTER_TOKEN
---

# Google Keep Skill

Manage Google Keep notes, lists, and checklists using the built-in Python CLI wrapper (`gkeep.py`).

## Environment Variables Setup

Configure the following environment variables in your system environment (e.g., `~/.zshrc`, `~/.bashrc`, or system environment variables):

- `GOOGLE_KEEP_EMAIL`: Your Google account email address (e.g. `user@gmail.com`).
- `GOOGLE_KEEP_MASTER_TOKEN`: Your Google Keep master token (`aas_et/...`) or `GOOGLE_KEEP_OAUTH_TOKEN`.

> 💡 **Token Acquisition Guide**: For step-by-step instructions on obtaining and configuring your token, see `README.md`.

## CLI Usage

### List Notes
```bash
python3 skills/google-keep/gkeep.py list --json
python3 skills/google-keep/gkeep.py list --pinned
```

### Search Notes
```bash
python3 skills/google-keep/gkeep.py search "groceries" --json
```

### Get Note Details
```bash
python3 skills/google-keep/gkeep.py get "<note-id-or-title>" --json
```

### Create Note
```bash
python3 skills/google-keep/gkeep.py create --title "Meeting Notes" --text "Discuss Q3 roadmap" --json
```

### Create Checklist
```bash
python3 skills/google-keep/gkeep.py create --title "Shopping List" --list --items "Milk" "Eggs" "Bread" --json
```

### Add Item to Checklist
```bash
python3 skills/google-keep/gkeep.py add-item "Shopping List" "Apples"
```

### Delete Note
```bash
python3 skills/google-keep/gkeep.py delete "<note-id-or-title>"
```

## Automatic Setup & Auth

`gkeep.py` checks for `gkeepapi` and `gpsoauth` dependencies upon execution and auto-installs missing packages. Credentials are read directly from `GOOGLE_KEEP_EMAIL` and `GOOGLE_KEEP_MASTER_TOKEN` environment variables, avoiding any manual setup or login steps.
