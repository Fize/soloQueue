---
name: google-docs
description: |
  Google Docs API integration with managed OAuth via Maton. Create, read, search, edit, and format Google Docs online cloud documents.
  Use this skill ONLY for online Google Docs cloud documents (Google Workspace docs.google.com or Document IDs).
  Do NOT use this skill for local office files (.docx or .doc files on the local file system); use the docx skill for local Word files.
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

# Google Docs (Cloud Document Skill)

Access the Google Docs API with managed OAuth authentication via Maton. Create, read, update, format, and batch-edit Google Docs cloud documents.

> ⚠️ **Cloud vs. Local File Scope Distinction**:
> - **`google-docs` (This Skill)**: Exclusively for **online Google Docs cloud documents** (`docs.google.com/document/d/{documentId}`). Interacts directly with Google's cloud servers via Maton OAuth API proxy.
> - **`docx` Skill**: For reading, creating, or editing **local `.docx` / `.doc` files** stored on the local disk.

---

## Quick Start

**CLI:**

```bash
# Get Document structure & text content
maton api '/google-docs/v1/documents/<documentId>'
```

```bash
# Create a new Google Doc
maton api -X POST /google-docs/v1/documents -d '{"title": "Project Architecture Spec"}'
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/google-docs/v1/documents/<documentId>')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

---

## Base URL

```
https://api.maton.ai/google-docs/{native-api-path}
```

Maton proxies requests to `docs.googleapis.com` and automatically injects your OAuth token.

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

Manage your Google Docs OAuth connections at `https://api.maton.ai`.

### List Connections

**CLI:**

```bash
maton connection list google-docs --status ACTIVE
```

```bash
maton api -X GET /connections -f app=google-docs -f status=ACTIVE
```

### Create Connection

**CLI:**

```bash
maton connection create google-docs
```

```bash
maton api /connections -f app=google-docs
```

**Response:**

```json
{
  "connection": {
    "connection_id": "{connection_id}",
    "status": "ACTIVE",
    "url": "https://connect.maton.ai/?session_token=...",
    "app": "google-docs"
  }
}
```

Open the returned `url` in a browser to complete OAuth authorization.

---

## Security & Permissions

- Access is scoped to Google Docs within the connected Google account.
- **All write and delete operations require explicit user approval.** Confirm target resource ID and content modifications with the user before executing.

---

## API Reference

### 1. Get Document

Retrieve the full document object including title, structural elements, inline objects, and paragraph styling.

```bash
GET /google-docs/v1/documents/{documentId}
```

**Query Parameters:**
- `suggestionsViewMode` - How suggestions are treated: `DEFAULT_FOR_CURRENT_ACCESS`, `PREVIEW_APPROVED`, `PREVIEW_WITHOUT_SUGGESTIONS`, `SUGGESTIONS_INLINE`.

Example CLI:
```bash
maton api '/google-docs/v1/documents/<documentId>'
```

---

### 2. Create Blank Document

Create a new empty Google Doc with a specified title.

```bash
POST /google-docs/v1/documents
Content-Type: application/json

{
  "title": "Quarterly Technical Review"
}
```

Example CLI:
```bash
maton api -X POST /google-docs/v1/documents -d '{"title": "Quarterly Technical Review"}'
```

---

### 3. Batch Update Document Content & Structure

Perform atomic mutations on document text, formatting, paragraph styles, headers/footers, and inline tables.

```bash
POST /google-docs/v1/documents/{documentId}:batchUpdate
Content-Type: application/json

{
  "requests": [
    {
      "insertText": {
        "location": {
          "index": 1
        },
        "text": "Executive Summary\n\nThis document outlines our cloud migration strategy."
      }
    }
  ]
}
```

#### Common Request Types for `batchUpdate`:

1. **`insertText`**: Insert text at a specific index location.
2. **`deleteContentRange`**: Delete content within a specified `range` (`startIndex` and `endIndex`).
3. **`replaceAllText`**: Global search and replace text across document body.
   ```json
   {
     "replaceAllText": {
       "containsText": {
         "text": "{{PROJECT_NAME}}",
         "matchCase": true
       },
       "replaceText": "SoloQueue v2.0"
     }
   }
   ```
4. **`updateTextStyle`**: Apply bold, italic, font size, or color formatting to range.
5. **`updateParagraphStyle`**: Apply heading styles (`HEADING_1`, `HEADING_2`, `NORMAL_TEXT`).

---

## Extracting Plain Text from Document Structure

Google Docs API returns content inside `body.content` as structural elements. To read the plain text of a doc via Python:

```python
import os, json, urllib.request

req = urllib.request.Request('https://api.maton.ai/google-docs/v1/documents/<documentId>')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
doc = json.load(urllib.request.urlopen(req))

text_chunks = []
for element in doc.get('body', {}).get('content', []):
    if 'paragraph' in element:
        for elem in element['paragraph'].get('elements', []):
            if 'textRun' in elem:
                text_chunks.append(elem['textRun'].get('content', ''))

print("".join(text_chunks))
```

---

## Error Handling

| Status | Meaning |
|--------|---------|
| 400 | Missing Google Docs connection or malformed request payload |
| 401 | Invalid or missing Maton API key |
| 404 | Google Doc ID not found or access denied |
| 429 | Rate limit exceeded |
| 5xx | Google Docs API backend error |

---

## Resources

- [Google Docs REST API v1 Docs](https://developers.google.com/docs/api)
- [Google Docs Batch Update Reference](https://developers.google.com/docs/api/reference/rest/v1/documents/batchUpdate)
- [Maton CLI Documentation](https://cli.maton.ai)
