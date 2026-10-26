package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/channel/wecom"
	"github.com/xiaobaitu/soloqueue/internal/config"
)

func TestWecomNotificationChecksCurrentIdentityBindingAndWhitelist(t *testing.T) {
	cfg, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bot := config.WecomBotConfig{ID: "account", BotID: "bot", Secret: "fake", Enabled: true, BindType: "l2", BindAgent: "devs", WhitelistEnabled: true, Whitelist: []string{"user"}}
	if err := cfg.UpdateWecomBots([]config.WecomBotConfig{bot}); err != nil {
		t.Fatal(err)
	}
	m := &WecomBotManager{cfg: cfg, clients: map[string]*wecom.Client{"account": wecom.NewClient(wecom.Config{}, nil)}}
	route := channel.Message{Channel: "wecom", AccountID: "account", AccountIdentity: "bot", UserID: "user", ConversationID: "group", ReplyToken: "unused"}
	check := func(ctx context.Context, msg channel.Message, want string) {
		t.Helper()
		data, _ := json.Marshal(msg)
		err := m.sendNotification(ctx, data, "result")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err=%v want=%s", err, want)
		}
	}
	wrongAccount := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "other", Group: "devs"})
	check(wrongAccount, route, "binding mismatch")
	wrongGroup := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "account", Group: "other"})
	check(wrongGroup, route, "group mismatch")
	ctx := channel.ContextWithNotificationBinding(context.Background(), channel.NotificationBinding{AccountID: "account", Group: "devs"})
	denied := route
	denied.UserID = "denied"
	check(ctx, denied, "recipient denied")
	changed := route
	changed.AccountIdentity = "old-bot"
	check(ctx, changed, "identity changed")
	bot.Enabled = false
	if err := cfg.UpdateWecomBots([]config.WecomBotConfig{bot}); err != nil {
		t.Fatal(err)
	}
	check(ctx, route, "account unavailable")
}
