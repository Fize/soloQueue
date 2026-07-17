package wechat

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

// WechatNotifier implements channel.ChannelNotifier for WeChat notification delivery.
// It uses the WeChat client's SendText method to deliver cron task completion
// notifications to the user through their channel binding.
type WechatNotifier struct {
	// Client is the WeChat iLink client used for message sending.
	Client *Client
}

// compile-time interface check
var _ channel.ChannelNotifier = (*WechatNotifier)(nil)

// SendNotification sends a text notification to a WeChat user.
// userID maps to the WeChat user identifier, conversationID maps to the ReplyToken
// of the original conversation context.
func (n *WechatNotifier) SendNotification(ctx context.Context, userID, conversationID, text string) error {
	if n.Client == nil {
		return fmt.Errorf("WechatNotifier: client is nil")
	}

	msg := channel.Message{
		UserID:     userID,
		ReplyToken: conversationID,
	}

	return n.Client.SendText(ctx, msg, text)
}
