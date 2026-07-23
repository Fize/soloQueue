---
name: google-sheets
description: |
  Google Sheets API integration with managed OAuth via Maton. Create, read, search, update, append, and format Google Sheets online cloud spreadsheets.
  Use this skill ONLY for online Google Sheets cloud spreadsheets (Google Workspace docs.google.com/spreadsheets or Spreadsheet IDs).
  Do NOT use this skill for local spreadsheet files (.xlsx, .xls, or .csv files on the local file system); use the xlsx skill for local Excel files.
  For other third party apps, use the api-gateway skill (https://clawhub.ai/byungkyu/api-gateway).
compatibility: Requires network access and valid Maton API key
metadata:
  author: maton
  version: "1.0"
  clawdbot:
    requires:
      env:
        - MATON_API_KEY
---

# Google Sheets (Cloud Spreadsheet Skill)

Access the Google Sheets API (v4) with managed OAuth authentication via Maton. Create spreadsheets, read cell values, update data, append rows, and manage formulas/formatting.

> ⚠️ **Cloud vs. Local File Scope Distinction**:
> - **`google-sheets` (This Skill)**: Exclusively for **online Google Sheets cloud spreadsheets** (`docs.google.com/spreadsheets/d/{spreadsheetId}`). Interacts directly with Google's cloud servers via Maton OAuth API proxy.
> - **`xlsx` Skill**: For reading, creating, or editing **local `.xlsx` / `.xls` / `.csv` files** stored on the local disk.

---

## Quick Start

**CLI:**

```bash
# Read values from a sheet range
maton api '/google-sheets/v4/spreadsheets/<spreadsheetId>/values/Sheet1!A1:D10'
```

```bash
# Update values in a sheet range
maton api -X PUT '/google-sheets/v4/spreadsheets/<spreadsheetId>/values/Sheet1!A1:B2?valueInputOption=USER_ENTERED' \
  -d '{"values": [["Name", "Score"], ["Alice", 95]]}'
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/google-sheets/v4/spreadsheets/<spreadsheetId>/values/Sheet1!A1:Z100')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

---

## Base URL

```
https://api.maton.ai/google-sheets/{native-api-path}
```

Maton proxies requests to `sheets.googleapis.com` and automatically injects your OAuth token.

---

## CLI Installation (optional — agent decides)

Maton CLI is optional. Agent can decide whether to install it based on the environment.

### macOS / Linux

```bash
curl -fsSL https://maton.ai/install.sh | bash
```

### Windows

```powershell
irm https://maton.ai/install.ps1 | iex
```

### npm (cross-platform)

```bash
npm install -g @maton/cli
```

---

## Authentication

**CLI:**

```bash
maton login                          # Opens browser for API key
maton login --interactive            # Skip browser, paste API key directly
maton whoami                         # Show current auth state
```

**Manual:**

1. Sign in or create an account at [maton.ai](https://maton.ai)
2. Go to [maton.ai/settings](https://maton.ai/settings)
3. Copy your API key
4. Set your API key as `MATON_API_KEY`:

```bash
export MATON_API_KEY="YOUR_API_KEY"
```

---

## Connection Management

Manage your Google Sheets OAuth connections at `https://api.maton.ai`.

### List Connections

**CLI:**

```bash
maton connection list google-sheets --status ACTIVE
```

```bash
maton api -X GET /connections -f app=google-sheets -f status=ACTIVE
```

### Create Connection

**CLI:**

```bash
maton connection create google-sheets
```

```bash
maton api /connections -f app=google-sheets
```

---

## Security & Permissions

- Access is scoped to Google Sheets within the connected Google account.
- **All write, update, append, and delete operations require explicit user approval.** Confirm target spreadsheet ID, sheet name, and cell values with the user before executing.

---

## API Reference

### 1. Get Spreadsheet Metadata

Retrieve spreadsheet properties, sheets list, grid dimensions, and formulas.

```bash
GET /google-sheets/v4/spreadsheets/{spreadsheetId}
```

Example CLI:
```bash
maton api '/google-sheets/v4/spreadsheets/<spreadsheetId>'
```

---

### 2. Read Cell Values

Read cell values from a specified range (e.g., `Sheet1!A1:C5` or `A:D`).

```bash
GET /google-sheets/v4/spreadsheets/{spreadsheetId}/values/{range}
```

**Query Parameters:**
- `valueRenderOption` - `FORMATTED_VALUE`, `UNFORMATTED_VALUE`, or `FORMULA`
- `dateTimeRenderOption` - `SERIAL_NUMBER` or `FORMATTED_STRING`

Example CLI:
```bash
maton api '/google-sheets/v4/spreadsheets/<spreadsheetId>/values/Sheet1!A1:E50?valueRenderOption=FORMATTED_VALUE'
```

---

### 3. Update Cell Range

Write or replace cell values in a range.

```bash
PUT /google-sheets/v4/spreadsheets/{spreadsheetId}/values/{range}?valueInputOption=USER_ENTERED
Content-Type: application/json

{
  "range": "Sheet1!A1:B2",
  "majorDimension": "ROWS",
  "values": [
    ["Item", "Price"],
    ["Widget", 19.99]
  ]
}
```

- `valueInputOption`: `USER_ENTERED` (parses numbers, dates, formulas like `=SUM(...)`) or `RAW` (stores raw strings).

---

### 4. Append Rows to Sheet

Append new rows after the last row of data in a table range.

```bash
POST /google-sheets/v4/spreadsheets/{spreadsheetId}/values/{range}:append?valueInputOption=USER_ENTERED&insertDataOption=INSERT_ROWS
Content-Type: application/json

{
  "values": [
    ["2026-07-23", "Subscription Revenue", 1250.00]
  ]
}
```

---

### 5. Create New Spreadsheet

Create a blank spreadsheet with initial title and sheets.

```bash
POST /google-sheets/v4/spreadsheets
Content-Type: application/json

{
  "properties": {
    "title": "Q3 Financial Report"
  },
  "sheets": [
    {
      "properties": {
        "title": "Summary"
      }
    }
  ]
}
```

---

### 6. Batch Update Sheet Formatting & Layout

Add sheets, delete sheets, change colors, format borders, update column widths, or set grid cell properties.

```bash
POST /google-sheets/v4/spreadsheets/{spreadsheetId}:batchUpdate
Content-Type: application/json

{
  "requests": [
    {
      "addSheet": {
        "properties": {
          "title": "Analytics Data"
        }
      }
    }
  ]
}
```

---

## Code Examples

### Python: Read Values & Convert to Dict

```python
import os, json, urllib.request

spreadsheet_id = "<spreadsheetId>"
cell_range = "Sheet1!A1:C10"

url = f"https://api.maton.ai/google-sheets/v4/spreadsheets/{spreadsheet_id}/values/{cell_range}"
req = urllib.request.Request(url)
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')

res = json.load(urllib.request.urlopen(req))
rows = res.get('values', [])

if rows:
    headers = rows[0]
    data = [dict(zip(headers, row)) for row in rows[1:]]
    print(json.dumps(data, indent=2))
```

---

## Error Handling

| Status | Meaning |
|--------|---------|
| 400 | Missing Google Sheets connection or invalid range syntax (e.g. `Sheet1!A1:Z`) |
| 401 | Invalid or missing Maton API key |
| 404 | Spreadsheet ID not found or access denied |
| 429 | Rate limit exceeded |
| 5xx | Google Sheets API backend error |

---

## Resources

- [Google Sheets REST API v4 Reference](https://developers.google.com/sheets/api/reference/rest)
- [Values API Guide](https://developers.google.com/sheets/api/guides/values)
- [Maton CLI Manual](https://cli.maton.ai)
