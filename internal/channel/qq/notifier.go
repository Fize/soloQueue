package qq

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

// QQNotifier implements channel.ChannelNotifier for QQ Bot notification delivery.
// It uses the QQ bot's active-message path to send notifications without requiring
// a conversation thread context.
type QQNotifier struct {
	// Bridge is the QQ session bridge used for active message sending.
	Bridge *SessionBridge
}

// compile-time interface check
var _ channel.ChannelNotifier = (*QQNotifier)(nil)

// SendNotification sends a text notification to a QQ user.
// userID maps to OpenID, conversationID maps to ChatID.
// The message is sent as a markdown-formatted active message (not a threaded reply).
func (n *QQNotifier) SendNotification(ctx context.Context, userID, conversationID, text string) error {
	if n.Bridge == nil {
		return fmt.Errorf("QQNotifier: bridge is nil")
	}

	msg := QQMessage{
		Source: MessageSource(chatIDToSource(conversationID)),
		OpenID: userID,
		ChatID: conversationID,
	}
	formatted := QQMarkdown(text)

	return n.Bridge.SendActiveMessage(ctx, msg, MsgTypeMarkdown, formatted)
}

// chatIDToSource infers the QQ message source from the conversation ID prefix.
// Defaults to C2C (private chat).
func chatIDToSource(chatID string) int {
	// Group chats typically have longer IDs; private chat IDs are shorter.
	// If no chatID, default to C2C.
	if len(chatID) > 20 {
		return int(SourceGroup)
	}
	return int(SourceC2C)
}
