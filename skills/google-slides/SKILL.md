---
name: google-slides
description: |
  Google Slides API integration with managed OAuth via Maton. Create, read, edit, and update Google Slides online cloud presentations.
  Use this skill ONLY for online Google Slides cloud presentations (Google Workspace docs.google.com/presentation or Presentation IDs).
  Do NOT use this skill for local presentation files (.pptx or .ppt files on the local file system); use the pptx skill for local PowerPoint files.
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

# Google Slides (Cloud Presentation Skill)

Access the Google Slides API (v1) with managed OAuth authentication via Maton. Create presentations, inspect slide layouts, insert shapes/text/images, and update slide content.

> ⚠️ **Cloud vs. Local File Scope Distinction**:
> - **`google-slides` (This Skill)**: Exclusively for **online Google Slides cloud presentations** (`docs.google.com/presentation/d/{presentationId}`). Interacts directly with Google's cloud servers via Maton OAuth API proxy.
> - **`pptx` Skill**: For reading, creating, or editing **local `.pptx` / `.ppt` files** stored on the local disk.

---

## Quick Start

**CLI:**

```bash
# Get Presentation structure & slides
maton api '/google-slides/v1/presentations/<presentationId>'
```

```bash
# Create a new Google Presentation
maton api -X POST /google-slides/v1/presentations -d '{"title": "Q3 Keynote Pitch"}'
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/google-slides/v1/presentations/<presentationId>')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

---

## Base URL

```
https://api.maton.ai/google-slides/{native-api-path}
```

Maton proxies requests to `slides.googleapis.com` and automatically injects your OAuth token.

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

Manage your Google Slides OAuth connections at `https://api.maton.ai`.

### List Connections

**CLI:**

```bash
maton connection list google-slides --status ACTIVE
```

```bash
maton api -X GET /connections -f app=google-slides -f status=ACTIVE
```

### Create Connection

**CLI:**

```bash
maton connection create google-slides
```

```bash
maton api /connections -f app=google-slides
```

---

## Security & Permissions

- Access is scoped to Google Slides within the connected Google account.
- **All write, update, create slide, and delete operations require explicit user approval.** Confirm target presentation ID and modification details with the user before executing.

---

## API Reference

### 1. Get Presentation

Retrieve presentation metadata, total slides count, page elements (shapes, text frames, images, tables), and slide dimensions.

```bash
GET /google-slides/v1/presentations/{presentationId}
```

Example CLI:
```bash
maton api '/google-slides/v1/presentations/<presentationId>'
```

---

### 2. Create Presentation

Create a blank presentation.

```bash
POST /google-slides/v1/presentations
Content-Type: application/json

{
  "title": "Product Roadmap 2026"
}
```

---

### 3. Batch Update Slides & Page Elements

Add slides, delete slides, insert text, create shapes, add images, or transform element positioning on pages.

```bash
POST /google-slides/v1/presentations/{presentationId}:batchUpdate
Content-Type: application/json

{
  "requests": [
    {
      "createSlide": {
        "objectId": "slide_page_02",
        "insertionIndex": 1,
        "slideLayout": {
          "predefinedLayout": "TITLE_AND_BODY"
        }
      }
    }
  ]
}
```

#### Common Request Types for `batchUpdate`:

1. **`createSlide`**: Add a new slide page with a layout (`TITLE`, `TITLE_AND_BODY`, `SECTION_HEADER`, `BLANK`).
2. **`createShape`**: Add shapes (rectangles, text boxes, callouts) to a slide page.
3. **`insertText`**: Insert text into a shape or text box by `objectId`.
4. **`replaceAllText`**: Search and replace text across all slides in the deck.
   ```json
   {
     "replaceAllText": {
       "containsText": {
         "text": "{{COMPANY}}",
         "matchCase": true
       },
       "replaceText": "Acme Corp"
     }
   }
   ```
5. **`createImage`**: Add an image element to a slide given an image URL.
6. **`deleteObject`**: Delete a slide, shape, image, or table element by ID.

---

## Code Examples

### Python: List Slide Titles in a Presentation

```python
import os, json, urllib.request

presentation_id = "<presentationId>"
url = f"https://api.maton.ai/google-slides/v1/presentations/{presentation_id}"

req = urllib.request.Request(url)
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')

pres = json.load(urllib.request.urlopen(req))
slides = pres.get('slides', [])

print(f"Presentation Title: {pres.get('title')}")
print(f"Total Slides: {len(slides)}")

for idx, slide in enumerate(slides, 1):
    slide_id = slide.get('objectId')
    page_elements = slide.get('pageElements', [])
    print(f"Slide {idx} (ID: {slide_id}): {len(page_elements)} elements")
```

---

## Error Handling

| Status | Meaning |
|--------|---------|
| 400 | Missing Google Slides connection or invalid batchUpdate request schema |
| 401 | Invalid or missing Maton API key |
| 404 | Presentation ID not found or access denied |
| 429 | Rate limit exceeded |
| 5xx | Google Slides API backend error |

---

## Resources

- [Google Slides REST API v1 Reference](https://developers.google.com/slides/api/reference/rest)
- [Slides API Structural Overview](https://developers.google.com/slides/api/guides/presentation)
- [Maton CLI Manual](https://cli.maton.ai)
