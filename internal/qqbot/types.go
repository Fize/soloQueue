package qqbot

import (
	"encoding/json"
	"strings"
)

// ─── OpCode constants ────────────────────────────────────────────────────────

const (
	OpDispatch        = 0  // S→C: server push event
	OpHeartbeat      = 1  // C→S: client heartbeat
	OpIdentify       = 2  // C→S: authenticate
	OpResume         = 6  // C→S: resume session
	OpReconnect      = 7  // S→C: server requests reconnect
	OpHello          = 10 // S→C: connection established, carries heartbeat interval
	OpHeartbeatACK   = 11 // S→C: heartbeat acknowledged
	OpInvalidSession = 9  // S→C: session expired, client must Identify again
	OpCallbackVerify = 13 // S→C: webhook callback verification (not used in WS mode)
)

// ─── Intents ─────────────────────────────────────────────────────────────────

const (
	IntentGuilds              = 1 << 0  // GUILD_CREATE/UPDATE/DELETE, CHANNEL_CREATE/UPDATE/DELETE
	IntentGuildMembers        = 1 << 1  // GUILD_MEMBER_ADD/UPDATE/REMOVE
	IntentGuildMessages       = 1 << 9  // MESSAGE_CREATE/DELETE (private bot only)
	IntentGuildMessageReactions = 1 << 10
	IntentDirectMessage       = 1 << 12 // DIRECT_MESSAGE_CREATE/DELETE
	IntentGroupAndC2CEvent    = 1 << 25 // C2C_MESSAGE_CREATE, GROUP_AT_MESSAGE_CREATE, etc.
	IntentInteraction         = 1 << 26
	IntentMessageAudit        = 1 << 27
	IntentForumsEvent         = 1 << 28
	IntentAudioAction         = 1 << 29
	IntentPublicGuildMessages = 1 << 30 // AT_MESSAGE_CREATE
)

// DefaultIntents returns the recommended intents for a typical bot:
// GROUP_AND_C2C_EVENT + PUBLIC_GUILD_MESSAGES
func DefaultIntents() int {
	return IntentGroupAndC2CEvent | IntentPublicGuildMessages
}

// ─── Gateway Payload ─────────────────────────────────────────────────────────

// GatewayPayload is the unified message format for WebSocket uplink/downlink.
type GatewayPayload struct {
	Op   int             `json:"op"`
	S    *int            `json:"s,omitempty"`    // sequence number (downlink only)
	T    string          `json:"t,omitempty"`    // event type (op=0 only)
	D    json.RawMessage `json:"d"`              // event data
	ID   string          `json:"id,omitempty"`   // event id
}

// ─── Hello ───────────────────────────────────────────────────────────────────

// HelloData is the data payload for OpCode 10 Hello.
type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"` // milliseconds
}

// ─── Identify ────────────────────────────────────────────────────────────────

// IdentifyData is the data payload for OpCode 2 Identify.
type IdentifyData struct {
	Token      string                 `json:"token"`
	Intents   int                    `json:"intents"`
	Shard     [2]int                 `json:"shard"`
	Properties map[string]string     `json:"properties,omitempty"`
}

// ─── Resume ──────────────────────────────────────────────────────────────────

// ResumeData is the data payload for OpCode 6 Resume.
type ResumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int    `json:"seq"`
}

// ─── Heartbeat ───────────────────────────────────────────────────────────────

// HeartbeatData is the data payload for OpCode 1 Heartbeat.
// The value is the last received sequence number, or nil for the first heartbeat.
type HeartbeatData = *int

// ─── Ready Event ─────────────────────────────────────────────────────────────

// ReadyData is the data payload for the READY event (op=0, t=READY).
type ReadyData struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"user"`
	Shard [2]int `json:"shard"`
}

// ─── Resumed Event ───────────────────────────────────────────────────────────


// ─── Attachment ──────────────────────────────────────────────────────────────

// QQAttachment represents a message attachment (image, file, etc.).
type QQAttachment struct {
	ContentType string `json:"content_type"` // e.g. "image/png", "image/jpeg"
	URL         string `json:"url"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// ImageURLs returns image URLs from a slice of attachments.
func ImageURLs(atts []QQAttachment) []string {
	var urls []string
	for _, a := range atts {
		if strings.Contains(a.ContentType, "image") && a.URL != "" {
			urls = append(urls, a.URL)
		}
	}
	return urls
}

// ─── Message Events ──────────────────────────────────────────────────────────

// EventType constants for dispatch events.
const (
	EventC2CMessageCreate       = "C2C_MESSAGE_CREATE"
	EventGroupAtMessageCreate   = "GROUP_AT_MESSAGE_CREATE"
	EventDirectMessageCreate    = "DIRECT_MESSAGE_CREATE"
	EventPublicAtMessageCreate  = "AT_MESSAGE_CREATE" // public guild message
	EventReady                  = "READY"
	EventResumed                = "RESUMED"
)

// C2CMessageEvent represents a private chat (C2C) message event.
type C2CMessageEvent struct {
	ID        string `json:"id"`
	Author    struct {
		MemberOpenid string `json:"member_openid"`
		UserOpenid   string `json:"user_openid"`
	} `json:"author"`
	Content     string          `json:"content"`
	Attachments []QQAttachment  `json:"attachments,omitempty"`
	Timestamp   string          `json:"timestamp"`
	SeqInChat   int             `json:"seq_in_chat,omitempty"`
}

// GroupAtMessageEvent represents a group @bot message event.
type GroupAtMessageEvent struct {
	ID        string `json:"id"`
	Author    struct {
		MemberOpenid string `json:"member_openid"`
		UserOpenid   string `json:"user_openid"`
	} `json:"author"`
	Content     string          `json:"content"`
	Attachments []QQAttachment  `json:"attachments,omitempty"`
	GroupOpenid string          `json:"group_openid"`
	Timestamp   string          `json:"timestamp"`
	SeqInChat   int             `json:"seq_in_chat,omitempty"`
}

// DirectMessageEvent represents a guild direct message event.
type DirectMessageEvent struct {
	ID        string `json:"id"`
	Author    struct {
		UserOpenid string `json:"user_openid"`
	} `json:"author"`
	Content     string          `json:"content"`
	Attachments []QQAttachment  `json:"attachments,omitempty"`
	ChannelID   string          `json:"channel_id"`
	Timestamp   string          `json:"timestamp"`
	SeqInChat   int             `json:"seq_in_chat,omitempty"`
}

// PublicAtMessageEvent represents a public guild @bot message event.
type PublicAtMessageEvent struct {
	ID        string `json:"id"`
	Author    struct {
		MemberOpenid string `json:"member_openid"`
		UserOpenid   string `json:"user_openid"`
	} `json:"author"`
	Content     string          `json:"content"`
	Attachments []QQAttachment  `json:"attachments,omitempty"`
	ChannelID   string          `json:"channel_id"`
	GuildID     string          `json:"guild_id"`
	Timestamp   string          `json:"timestamp"`
	SeqInChat   int             `json:"seq_in_chat,omitempty"`
}

// ─── Unified QQMessage ───────────────────────────────────────────────────────

// MessageSource indicates where a QQ message originated.
type MessageSource int

const (
	SourceC2C       MessageSource = iota // private chat
	SourceGroup                          // group @bot
	SourceDirect                         // guild direct message
	SourcePublicGuild                    // public guild @bot
)

// QQMessage is a unified message representation extracted from various QQ events.
// It abstracts away the event-type-specific fields so the handler layer
// doesn't need to know about the underlying protocol.
type QQMessage struct {
	Source       MessageSource
	Content      string // user message text
	ImageURLs    []string // attachment image URLs
	OpenID       string // user openid (C2C/group member)
	TargetOpenID string // target openid for reply: user openid (C2C) or group openid (group)
	ChatID       string // group_openid / channel_id / user_openid depending on source
	EventID      string // event id, used for message reference in replies
	Seq          int    // seq_in_chat, used for passive message reply
}
