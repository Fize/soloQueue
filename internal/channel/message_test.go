package channel

import (
	"context"
	"testing"
)

func TestContextWithChatMeta_RoundTrip(t *testing.T) {
	meta := ChatMeta{
		Channel:        "qq",
		UserID:         "user-123",
		ConversationID: "conv-456",
	}
	ctx := ContextWithChatMeta(context.Background(), meta)

	got, ok := ChatMetaFromContext(ctx)
	if !ok {
		t.Fatal("ChatMetaFromContext returned false")
	}
	if got.Channel != "qq" {
		t.Errorf("Channel = %q, want %q", got.Channel, "qq")
	}
	if got.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user-123")
	}
	if got.ConversationID != "conv-456" {
		t.Errorf("ConversationID = %q, want %q", got.ConversationID, "conv-456")
	}
}

func TestChatMetaFromContext_NoMeta(t *testing.T) {
	ctx := context.Background()
	_, ok := ChatMetaFromContext(ctx)
	if ok {
		t.Error("ChatMetaFromContext should return false for bare context")
	}
}

func TestContextWithChatMeta_Overwrite(t *testing.T) {
	meta1 := ChatMeta{Channel: "qq", UserID: "u1"}
	meta2 := ChatMeta{Channel: "wechat", UserID: "u2"}
	ctx := ContextWithChatMeta(context.Background(), meta1)
	ctx = ContextWithChatMeta(ctx, meta2)

	got, ok := ChatMetaFromContext(ctx)
	if !ok {
		t.Fatal("ChatMetaFromContext returned false")
	}
	if got.Channel != "wechat" {
		t.Errorf("Channel = %q, want %q (overwrite should prevail)", got.Channel, "wechat")
	}
}
