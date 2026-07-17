package wechat

import "testing"

func TestGatewayNormalizeTextAndVoiceTranscript(t *testing.T) {
	gateway := &Gateway{cfg: Config{BotID: "bot-1"}}
	got, ok := gateway.normalize(Message{
		FromUserID:   "user-1",
		SessionID:    "session-1",
		MessageType:  1,
		ContextToken: "context-1",
		ItemList: []MessageItem{
			{Type: 1, TextItem: &TextItem{Text: "hello"}},
			{Type: 3, VoiceItem: &VoiceItem{Text: "voice text"}},
		},
	})
	if !ok {
		t.Fatal("message was not normalized")
	}
	if got.Text != "hello\nvoice text" || got.ReplyToken != "context-1" || got.AccountID != "bot-1" || got.Channel != "wechat" {
		t.Fatalf("normalized message = %#v", got)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Kind != "audio" || got.Attachments[0].Transcript != "voice text" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func TestGatewayNormalizeAcceptsMediaOnlyMessage(t *testing.T) {
	gateway := &Gateway{}
	got, ok := gateway.normalize(Message{FromUserID: "user", MessageType: 1, ContextToken: "ctx", ItemList: []MessageItem{{Type: 3, VoiceItem: &VoiceItem{}}}})
	if !ok || len(got.Attachments) != 1 || got.Attachments[0].Kind != "audio" {
		t.Fatalf("media-only message = %#v, ok=%v", got, ok)
	}
}
