# QQ Bot Integration

**Location**: `internal/qqbot/` (WebSocket gateway, handler, message queue, markdown utilities)

SoloQueue supports integration with official Tencent QQ Bot APIs. The bot can listen to messages in private C2C chats, groups, and guild channels, forward user requests to the agent runtime, and reply with text, markdown, and generated media attachments.

---

## Architecture Overview

```
 ┌──────────────┐          WebSocket (JSON)         ┌──────────────┐
 │  QQ Gateway  │ <───────────────────────────────> │  QQ Servers  │
 └──────┬───────┘                                   └──────────────┘
        │
        │ OnQQMessage(ctx, msg)
        ▼
 ┌──────────────┐           AskStream()             ┌──────────────┐
 │ SessionBridge│ ────────────────────────────────> │ Agent Session│
 └──────┬───────┘                                   └──────────────┘
        │
        ├─ Passive Reply (First Chunk) ───➔ QQ API (Direct Send)
        │
        └─ Active Messages (Follow-ups) ──➔ Message Queue (Rate Limited) ➔ QQ API
```

---

## 1. WebSocket Gateway (`gateway.go`)

The `Gateway` manages a persistent, real-time WebSocket connection to the QQ Bot Gateway.

### Connection & Lifecycle Loop

1. **Dial**: Connect to the gateway URL via `gorilla/websocket`.
2. **Hello (OpCode 10)**: Read the server hello payload, extract the `HeartbeatInterval` value, and start the heartbeat ticker.
3. **Authentication**:
   - **Resume (OpCode 6)**: If a `sessionID` and sequence number (`seq`) exist from a previous connection, send a resume payload to replay missed gateway events.
   - **Identify (OpCode 2)**: Otherwise, perform a fresh authentication handshake, registering token credentials and requested gateway **Intents** (e.g. listening to group messages, private messages, guild channels).
4. **Heartbeat Loop**: Periodically send heartbeat frames (OpCode 1) containing the last received sequence number. If a heartbeat ACK is not received, force a reconnect.
5. **Auto-Reconnection**:
   - OpCode 7 (Reconnect) or connection drops trigger the auto-reconnect loop.
   - Handles `OpInvalidSession` by clearing the session state and executing a fresh Identify.

### Event Dispatcher

Parsed messages are dispatched asynchronously to handlers:

- **`EventC2CMessageCreate`**: Private 1-on-1 user chats.
- **`EventGroupAtMessageCreate`**: Bot mentioned (`@bot`) in group chats.
- **`EventDirectMessageCreate`**: Private guild direct messages.
- **`EventPublicAtMessageCreate`**: Bot mentioned in public guild channels.

---

## 2. Session Bridge (`handler.go`)

The `SessionBridge` connects the QQ event loop to the SoloQueue agent runtime. It implements the `EventHandler` interface.

### Message Processing Pipeline

1. **Local Slash Commands**: Checks if the message content matches local commands:
   - `/help`: Returns list of commands.
   - `/cancel`: Cancels the currently executing agent task.
   - `/clear`: Resets conversation history (appends a clear control event).
   - `/version`: Returns the application version.
2. **LLM Execution**: If not a local command, the bridge forwards the text and any user-uploaded image URLs to the session's **`AskStream`** API.
3. **Passive vs Active Replies**:
   - **QQ Constraint**: The QQ Bot API only allows **one passive reply** (a message referencing the user's incoming message ID so it appears threaded) per incoming message.
   - **Threaded First Chunk**: The bridge sends the first chunk of the LLM response as a passive reply via `ReplyMessage`.
   - **Active Follow-ups**: If the response is split (see below), subsequent chunks are sent as active messages (direct sends with no message reference).

---

## 3. Message Splitting & Markdown Formatting

### Message Splitting (`SplitMarkdown` / `splitMessage`)

Tencent QQ enforces a maximum character limit (approximately 2000 bytes) per API message payload.

- **Plain Text**: Split using `splitMessage` which divides the response, preferring to break at newline boundaries near the limit.
- **Markdown**: Split using `SplitMarkdown` which parses markdown structure to prevent breaking inside code blocks, bold text, or link tags, ensuring valid formatting in each sent chunk.

### Markdown Conversion (`markdown.go`)

QQ Markdown is a restricted subset of Github Flavored Markdown. The `QQMarkdown` function normalizes standard LLM outputs:

- Converts header sizes (e.g. converts `#` and `##` to headers supported by QQ).
- Sanitizes list tags and tables.
- Adjusts code block formatting.

---

## 4. Rich Media & Image Uploads

If the agent generates media files (e.g. from the `ImageGenerate` tool) or is configured to send voice/file attachments:

1. **Upload Phase**: The bridge calls `api.UploadFile` with the media's public URL or Base64 data.
2. **Target Resolution**: Maps the target upload destination to either `"user"` (for C2C/direct chats) or `"group"` (for group chats) according to the message source.
3. **Send Phase**: QQ returns a `file_info` token. The bridge sends this token to the conversation as a rich media message (`MsgTypeMedia`).

---

## 5. Rate-Limiting Message Queue (`ratelimit.go`)

To avoid triggering Tencent's API rate limits when sending multi-chunk text or multiple media attachments, SoloQueue uses a buffered rate-limited queue:

- **Configuration**: Active follow-up messages are pushed to a `MessageQueue` rather than sent immediately.
- **Worker Loop**: A background worker goroutine processes queued send tasks sequentially, introducing a throttle delay (e.g. 1 second) between consecutive sends to the same conversation.
- **Graceful Degradation**: If the queue fills up, new tasks degrade gracefully or block to prevent memory exhaustion.
