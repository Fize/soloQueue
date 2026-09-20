package telegram

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

func TestGatewayDispatchPropagatesChatMeta(t *testing.T) {
	var gotCtx context.Context
	var gotMessage channel.Message
	gateway := NewGateway(
		Config{AccountID: "bot-account"},
		nil,
		channel.HandlerFunc(func(ctx context.Context, message channel.Message) {
			gotCtx = ctx
			gotMessage = message
		}),
		nil,
	)

	gateway.dispatch(context.Background(), Update{Message: &Message{
		MessageID: 23,
		From:      &User{ID: 45},
		Chat:      Chat{ID: 67},
		Text:      "status?",
	}})

	if gotMessage.Text != "status?" {
		t.Fatalf("message = %#v", gotMessage)
	}
	meta, ok := channel.ChatMetaFromContext(gotCtx)
	if !ok {
		t.Fatal("handler context has no chat metadata")
	}
	if meta.Channel != "telegram" || meta.AccountID != "bot-account" || meta.UserID != "45" || meta.ConversationID != "67" {
		t.Fatalf("chat metadata = %#v", meta)
	}
}
