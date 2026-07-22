---
name: google-calendar
description: |
  Google Calendar API integration with managed OAuth. Manage calendars and events with full CRUD operations.
  Use this skill when users want to read, create, update, or delete calendars and events in Google Calendar.
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

# Google Calendar

Access the Google Calendar API with managed OAuth authentication. Manage calendars, events, agenda views, ACLs, colors, free/busy queries, and settings.

## Quick Start

**CLI:**

```bash
maton google-calendar calendar list
```

```bash
maton google-calendar event list -c primary --time-min 2026-07-22T00:00:00Z --time-max 2026-07-23T00:00:00Z
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/google-calendar/calendar/v3/users/me/calendarList')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

## Base URL

```
https://api.maton.ai/google-calendar/{native-api-path}
```

Maton proxies requests to `calendar.googleapis.com` and automatically injects your OAuth token.

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

### Homebrew (macOS)

```bash
brew install maton-ai/cli/maton
```

After installation, verify with:

```bash
maton --version
```

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

## Connection Management

Manage your Google Calendar OAuth connections at `https://api.maton.ai`.

### List Connections

**CLI:**

```bash
maton connection list google-calendar --status ACTIVE
```

```bash
maton api -X GET /connections -f app=google-calendar -f status=ACTIVE
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/connections?app=google-calendar&status=ACTIVE')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

### Create Connection

**CLI:**

```bash
maton connection create google-calendar
```

```bash
maton api /connections -f app=google-calendar
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
data = json.dumps({'app': 'google-calendar'}).encode()
req = urllib.request.Request('https://api.maton.ai/connections', data=data, method='POST')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
req.add_header('Content-Type', 'application/json')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

### Get Connection

**CLI:**

```bash
maton connection view {connection_id}
```

```bash
maton api /connections/{connection_id}
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/connections/{connection_id}')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

**Response:**
```json
{
  "connection": {
    "connection_id": "{connection_id}",
    "status": "ACTIVE",
    "creation_time": "2026-02-07T02:35:51.002199Z",
    "last_updated_time": "2026-02-07T05:32:30.369186Z",
    "url": "https://connect.maton.ai/?session_token=...",
    "app": "google-calendar",
    "metadata": {}
  }
}
```

Open the returned `url` in a browser to complete OAuth authorization.

### Delete Connection

**CLI:**

```bash
maton connection delete {connection_id}
```

```bash
maton api -X DELETE /connections/{connection_id}
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/connections/{connection_id}', method='DELETE')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

### Specifying Connection

If you have multiple Google Calendar connections, specify which one to use:

**CLI:**

```bash
maton google-calendar calendar list --connection {connection_id}
```

```bash
maton api /google-calendar/calendar/v3/users/me/calendarList --connection {connection_id}
```

**Python:**

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/google-calendar/calendar/v3/users/me/calendarList')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
req.add_header('Maton-Connection', '{connection_id}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

If you have multiple connections, always specify the connection to ensure requests go to the intended account.

## Security & Permissions

- Access is scoped to calendars and events with full CRUD operations within the connected Google Calendar account.
- **All write operations require explicit user approval.** Before executing any create, update, or delete call, confirm the target resource and intended effect with the user.

## API Reference

### Calendar

#### List All Calendars

```bash
GET /google-calendar/calendar/v3/users/me/calendarList
```

**Query Parameters:**
- `maxResults` - Maximum entries to return
- `pageToken` - Token for pagination
- `showDeleted` - Include deleted calendars
- `showHidden` - Include hidden calendars

Example:

```bash
maton google-calendar calendar list
```

#### Get Calendar

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}
```

Example:

```bash
maton google-calendar calendar get primary
```

#### Create Calendar

```bash
POST /google-calendar/calendar/v3/calendars
Content-Type: application/json

{
  "summary": "Project X",
  "timeZone": "America/Los_Angeles"
}
```

Example:

```bash
maton google-calendar calendar create --summary 'Project X' --timezone America/Los_Angeles
```

#### Update Calendar (PATCH)

```bash
PATCH /google-calendar/calendar/v3/calendars/{calendarId}
Content-Type: application/json

{
  "summary": "Updated Title",
  "timeZone": "Asia/Shanghai"
}
```

Example:

```bash
maton google-calendar calendar update primary --summary 'Updated Title' --timezone Asia/Shanghai
```

#### Delete Calendar

```bash
DELETE /google-calendar/calendar/v3/calendars/{calendarId}
```

Example:

```bash
maton google-calendar calendar delete {calendarId}
```

#### Clear Calendar

Delete all events from a primary calendar.

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/clear
```

Example:

```bash
maton google-calendar calendar clear primary
```

#### Subscribe to Calendar

Add a calendar to the user's calendar list (does not create a new calendar).

```bash
POST /google-calendar/calendar/v3/users/me/calendarList
Content-Type: application/json

{
  "id": "en.usa#holiday@group.v.calendar.google.com"
}
```

Example:

```bash
maton google-calendar calendar subscribe en.usa#holiday@group.v.calendar.google.com
```

#### Unsubscribe from Calendar

Remove a calendar from the user's calendar list (does not delete the calendar itself).

```bash
DELETE /google-calendar/calendar/v3/users/me/calendarList/{calendarId}
```

Example:

```bash
maton google-calendar calendar unsubscribe en.usa#holiday@group.v.calendar.google.com
```

---

### Event

#### List Events

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}/events
```

**Query Parameters:**
- `maxResults` - Max events per page (1-2500)
- `pageToken` - Pagination token
- `showDeleted` - Include deleted events
- `showHiddenInvitations` - Include hidden invitations
- `singleEvents` - Expand recurring events into instances (default: true)
- `orderBy` - `startTime` or `updated`
- `q` - Free text search across event fields
- `timeMin` - Lower bound (RFC 3339)
- `timeMax` - Upper bound (RFC 3339)
- `updatedMin` - Filter to events updated after this timestamp
- `timeZone` - IANA timezone for response times

Example:

```bash
maton google-calendar event list -c primary
maton google-calendar event list -c primary --time-min 2026-07-22T00:00:00Z --time-max 2026-07-23T00:00:00Z
maton google-calendar event list -c primary --query 'standup' --paginate
```

#### Get Event

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}/events/{eventId}
```

Example:

```bash
maton google-calendar event get {eventId} -c primary
```

#### Create Event

**Required:** `--summary`, `--start` (RFC 3339), `--end` (RFC 3339)

**Optional:** `--description`, `--location`, `--attendee` (repeatable), `--meet` (Google Meet), `--send-updates` (all|externalOnly|none), `-c` (calendar ID, default "primary")

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/events
Content-Type: application/json

{
  "summary": "Standup",
  "start": {"dateTime": "2026-07-22T09:00:00Z"},
  "end": {"dateTime": "2026-07-22T09:30:00Z"},
  "attendees": [{"email": "alice@example.com"}],
  "location": "Room 301"
}
```

Example:

```bash
maton google-calendar event create --summary 'Standup' --start 2026-07-22T09:00:00Z --end 2026-07-22T09:30:00Z
maton google-calendar event create --summary 'Review' --start 2026-07-22T10:00:00Z --end 2026-07-22T11:00:00Z --attendee alice@example.com --attendee bob@example.com
maton google-calendar event create --summary 'Demo' --start 2026-07-22T14:00:00Z --end 2026-07-22T15:00:00Z --meet
```

#### Update Event (PATCH - partial update)

```bash
PATCH /google-calendar/calendar/v3/calendars/{calendarId}/events/{eventId}
Content-Type: application/json

{
  "summary": "New Title"
}
```

Example:

```bash
maton google-calendar event update {eventId} -c primary --summary 'New title'
```

#### Delete Event

```bash
DELETE /google-calendar/calendar/v3/calendars/{calendarId}/events/{eventId}
```

Example:

```bash
maton google-calendar event delete {eventId} -c primary
```

#### Move Event

Move an event to another calendar.

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/events/{eventId}/move?destination={destinationCalendarId}
```

Example:

```bash
maton google-calendar event move {eventId} -c primary --destination {otherCalendarId}
```

#### Quick Add Event

Natural language event creation.

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/events/quickAdd?text={text}
```

Example:

```bash
maton google-calendar event quick-add --text 'Lunch with Alice tomorrow at noon'
```

#### Import Event

Import an event from ICS/VCAL data.

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/events/import
Content-Type: application/json

{
  "summary": "Imported Event",
  "start": {"dateTime": "2026-07-22T09:00:00Z"},
  "end": {"dateTime": "2026-07-22T10:00:00Z"}
}
```

#### List Recurring Event Instances

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}/events/{eventId}/instances
```

Example:

```bash
maton google-calendar event instances {eventId} -c primary
```

---

### Agenda

Read-only listing of events from one or all calendars. Defaults to the next 1 day window.

**CLI flags:** `--today`, `--tomorrow`, `--week`, `--days <int>`, `--calendar <string>`, `--timezone <string>`, `--json`, `--paginate`

Example:

```bash
maton google-calendar agenda
maton google-calendar agenda --today
maton google-calendar agenda --week
maton google-calendar agenda --days 3 --timezone America/New_York
maton google-calendar agenda --calendar 'Work' --json
```

---

### ACL

#### List ACL Rules

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}/acl
```

Example:

```bash
maton google-calendar acl list -c primary
```

#### Get ACL

```bash
GET /google-calendar/calendar/v3/calendars/{calendarId}/acl/{ruleId}
```

Example:

```bash
maton google-calendar acl get {ruleId} -c primary
```

#### Create ACL

```bash
POST /google-calendar/calendar/v3/calendars/{calendarId}/acl
Content-Type: application/json

{
  "role": "reader",
  "scope": {
    "type": "user",
    "value": "alice@example.com"
  }
}
```

**Flags:** `--role` (reader|writer|owner|freeBusyReader), `--scope` (scope value)

Example:

```bash
maton google-calendar acl create -c primary --role reader --scope alice@example.com
```

#### Update ACL

```bash
PATCH /google-calendar/calendar/v3/calendars/{calendarId}/acl/{ruleId}
```

Example:

```bash
maton google-calendar acl update {ruleId} -c primary --role writer
```

#### Delete ACL

```bash
DELETE /google-calendar/calendar/v3/calendars/{calendarId}/acl/{ruleId}
```

Example:

```bash
maton google-calendar acl delete {ruleId} -c primary
```

---

### Colors

Get available calendar and event color palettes.

```bash
GET /google-calendar/calendar/v3/colors
```

Example:

```bash
maton google-calendar colors get
```

---

### Freebusy

Query busy time periods across calendars.

```bash
POST /google-calendar/calendar/v3/freeBusy
Content-Type: application/json

{
  "timeMin": "2026-07-22T00:00:00Z",
  "timeMax": "2026-07-23T00:00:00Z",
  "items": [{"id": "primary"}]
}
```

**Flags:** `--time-min` (RFC 3339), `--time-max` (RFC 3339), `--calendar` (repeatable)

Example:

```bash
maton google-calendar freebusy --time-min 2026-07-22T00:00:00Z --time-max 2026-07-23T00:00:00Z --calendar primary
maton google-calendar freebusy --time-min 2026-07-22T00:00:00Z --time-max 2026-07-23T00:00:00Z --calendar primary --calendar work@example.com
```

---

### Settings

Read Google Calendar user settings.

#### List Settings

```bash
GET /google-calendar/calendar/v3/users/me/settings
```

Example:

```bash
maton google-calendar settings list
```

#### Get Setting

```bash
GET /google-calendar/calendar/v3/users/me/settings/{setting}
```

Example:

```bash
maton google-calendar settings get timezone
maton google-calendar settings get locale
```

Common settings: `timezone`, `locale`, `weekStartDay`, `format24HourTime`, `defaultReminders`, `defaultEventLength`.

---

## Pagination

Google Calendar uses token-based pagination. The CLI automatically paginates with `--paginate`.

Example:

```bash
maton google-calendar event list -c primary --paginate
```

For raw API, pass `nextPageToken` as `pageToken` to get the next page.

## Event Resource Fields

| Field | Type | Description |
|-------|------|-------------|
| `kind` | string | Always "calendar#event" (output only) |
| `id` | string | Event identifier |
| `etag` | string | ETag of the resource |
| `status` | string | "confirmed", "tentative", or "cancelled" |
| `htmlLink` | string | Link to event in Google Calendar UI (output only) |
| `created` | string | Creation time (RFC 3339, output only) |
| `updated` | string | Last modification time (RFC 3339, output only) |
| `summary` | string | Event title |
| `description` | string | Event description |
| `location` | string | Event location |
| `colorId` | string | Color ID from the color palette |
| `creator` | object | Creator info (email, displayName, self) |
| `organizer` | object | Organizer info |
| `start` | object | Start time (dateTime + timeZone, or date for all-day) |
| `end` | object | End time |
| `endTimeUnspecified` | boolean | Whether end time is unspecified |
| `recurrence` | array | RRULE, EXRULE, RDATE, EXDATE lines |
| `recurringEventId` | string | ID of the parent recurring event |
| `originalStartTime` | object | Original start time of the instance |
| `attendees` | array | List of attendees (email, responseStatus, optional, etc.) |
| `attendeesOmitted` | boolean | Whether attendees may be omitted |
| `hangoutLink` | string | Google Meet link (output only) |
| `conferenceData` | object | Conference data (Meet, Hangouts, etc.) |
| `iCalUID` | string | iCalendar UID |
| `sequence` | integer | Sequence number |
| `reminders` | object | Reminder overrides (useDefault, overrides) |
| `source` | object | Source (url, title) |
| `attachments` | array | File attachments |
| `eventType` | string | "default", "outOfOffice", "focusTime", "workingLocation" |
| `privateCopy` | boolean | Whether it's a private copy |
| `guestsCanModify` | boolean | Whether guests can modify |
| `guestsCanSeeOtherGuests` | boolean | Whether guests can see other guests |
| `guestsCanInviteOthers` | boolean | Whether guests can invite others |
| `transparency` | string | "opaque" or "transparent" |
| `visibility` | string | "default", "public", "private", "confidential" |
| `anyoneCanAddSelf` | boolean | Whether anyone can add themselves |
| `locked` | boolean | Whether the event is locked |
| `outOfOfficeProperties` | object | Out-of-office properties |

## Code Examples

### CLI

```bash
# List all calendars
maton google-calendar calendar list

# Filter with jq — e.g., extract calendar summaries
maton google-calendar calendar list --json --jq '.items[].summary'

# List today's events
maton google-calendar agenda --today

# Create an event with attendees and Google Meet
maton google-calendar event create --summary 'Sprint Review' \
  --start 2026-07-22T15:00:00Z --end 2026-07-22T16:00:00Z \
  --attendee alice@example.com --attendee bob@example.com --meet

# Quick add with natural language
maton google-calendar event quick-add --text 'Coffee with Sarah next Monday at 10am'

# Check free/busy
maton google-calendar freebusy --time-min 2026-07-22T00:00:00Z --time-max 2026-07-23T00:00:00Z --calendar primary

# Get calendar colors
maton google-calendar colors get
```

### JavaScript

```javascript
// List all calendars
const response = await fetch(
  'https://api.maton.ai/google-calendar/calendar/v3/users/me/calendarList',
  {
    headers: {
      'Authorization': `Bearer ${process.env.MATON_API_KEY}`
    }
  }
);

// Create a new event
const createResponse = await fetch(
  `https://api.maton.ai/google-calendar/calendar/v3/calendars/primary/events`,
  {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${process.env.MATON_API_KEY}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      summary: 'Team Standup',
      start: { dateTime: '2026-07-22T09:00:00Z' },
      end: { dateTime: '2026-07-22T09:30:00Z' },
      attendees: [{ email: 'alice@example.com' }]
    })
  }
);
```

### Python

```python
import os
import requests

# List all calendars
response = requests.get(
    'https://api.maton.ai/google-calendar/calendar/v3/users/me/calendarList',
    headers={'Authorization': f'Bearer {os.environ["MATON_API_KEY"]}'}
)

# Create a new event
create_response = requests.post(
    'https://api.maton.ai/google-calendar/calendar/v3/calendars/primary/events',
    headers={'Authorization': f'Bearer {os.environ["MATON_API_KEY"]}'},
    json={
        'summary': 'Team Standup',
        'start': {'dateTime': '2026-07-22T09:00:00Z'},
        'end': {'dateTime': '2026-07-22T09:30:00Z'},
        'attendees': [{'email': 'alice@example.com'}]
    }
)
```

## Notes

- Calendar IDs are opaque strings; `primary` is the alias for the user's default calendar
- Event IDs are opaque strings (base64-encoded)
- Timestamps use RFC 3339 format: `2026-07-22T09:00:00Z` or `2026-07-22T09:00:00+08:00`
- IMPORTANT: When using curl commands, use `curl -g` when URLs contain brackets to disable glob parsing
- IMPORTANT: When piping curl output to `jq` or other commands, environment variables like `$MATON_API_KEY` may not expand correctly in some shell environments. You may get "Invalid API key" errors when piping.

## Error Handling

| Status | Meaning |
|--------|---------|
| 400 | Missing Google Calendar connection |
| 401 | Invalid or missing Maton API key |
| 404 | Calendar, event, or ACL rule not found |
| 409 | Conflict (e.g., trying to create a duplicate) |
| 429 | Rate limited |
| 4xx/5xx | Passthrough error from Google Calendar API |

### Troubleshooting: API Key Issues

**CLI:**

1. Check your auth state:

```bash
maton whoami
```

2. Verify the API key is valid by listing connections:

```bash
maton connection list
```

**Manual:**

1. Check that the `MATON_API_KEY` environment variable is set:

```bash
echo $MATON_API_KEY
```

2. Verify the API key is valid by listing connections:

```bash
python <<'EOF'
import urllib.request, os, json
req = urllib.request.Request('https://api.maton.ai/connections')
req.add_header('Authorization', f'Bearer {os.environ["MATON_API_KEY"]}')
print(json.dumps(json.load(urllib.request.urlopen(req)), indent=2))
EOF
```

### Troubleshooting: Invalid App Name

1. Ensure your URL path starts with `google-calendar`. For example:

- Correct: `https://api.maton.ai/google-calendar/calendar/v3/users/me/calendarList`
- Incorrect: `https://api.maton.ai/calendar/v3/users/me/calendarList`

## Resources

- [Google Calendar API Overview](https://developers.google.com/calendar/api/guides/overview)
- [Events Reference](https://developers.google.com/calendar/api/v3/reference/events)
- [Calendars Reference](https://developers.google.com/calendar/api/v3/reference/calendars)
- [Colors Reference](https://developers.google.com/calendar/api/v3/reference/colors)
- [Freebusy Reference](https://developers.google.com/calendar/api/v3/reference/freebusy)
- [Settings Reference](https://developers.google.com/calendar/api/v3/reference/settings)
- [ACL Reference](https://developers.google.com/calendar/api/v3/reference/acl)
- [Maton CLI Manual](https://cli.maton.ai/manual)
- [Maton Community](https://discord.com/invite/dBfFAcefs2)
- [Maton Support](mailto:support@maton.ai)
