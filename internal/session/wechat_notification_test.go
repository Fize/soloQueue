package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

type failedWechatRouteStore struct{}

func (failedWechatRouteStore) SaveChannelSenderData(string, string, string) error { return nil }
func (failedWechatRouteStore) GetChannelSenderData(string, string) (string, error) {
	return "", errors.New("fake-secret from storage")
}
func TestWechatNotificationMissingRoutesAreDiagnostic(t *testing.T) {
	oldText, oldMedia := channel.GetSenderFactory("wechat"), channel.GetMediaSenderFactory("wechat")
	defer channel.RegisterSenderFactory("wechat", oldText)
	defer channel.RegisterMediaSenderFactory("wechat", oldMedia)
	channel.RegisterSenderFactory("wechat", nil)
	channel.RegisterMediaSenderFactory("wechat", nil)
	database, err := db.Open(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, tc := range []struct {
		name     string
		store    ChannelMetadataStore
		metadata string
		want     string
	}{
		{"store missing", nil, "", "store_unavailable"},
		{"route missing", database, "", "route_missing"},
		{"store failure", failedWechatRouteStore{}, "", "route_load_failed"},
		{"factory missing", database, `{"AccountID":"bot","UserID":"recipient","ReplyToken":"fake-context"}`, "notification_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession("l1-session", "L1", &agent.Agent{Def: agent.Definition{NotifyChannel: "wechat"}}, nil, nil, nil)
			defer s.Close()
			s.metaStore = tc.store
			if tc.metadata != "" {
				if err := tc.store.SaveChannelSenderData(s.TargetID, "wechat", tc.metadata); err != nil {
					t.Fatal(err)
				}
			}
			for _, err := range []error{s.SendViaChannel(context.Background(), "text"), s.SendMediaViaChannel(context.Background(), []channel.OutboundMedia{{Kind: channel.MediaFile, Path: "unused"}})} {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("diagnostic = %v, want %s", err, tc.want)
				}
				if strings.Contains(err.Error(), "fake-secret") {
					t.Fatal("storage secret leaked")
				}
			}
		})
	}
}

func TestWecomNotificationRestoresGroupRouteWithCurrentBinding(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := NewSession("temporary-task", "devs", &agent.Agent{Def: agent.Definition{NotifyChannel: "wecom", Channels: map[string]string{"wecom": "account"}}}, nil, nil, nil)
	defer s.Close()
	s.metaStore = database
	s.notificationTargetID = "wecom-account"
	route := `{"Channel":"wecom","AccountID":"account","AccountIdentity":"bot","UserID":"recipient","ConversationID":"group"}`
	if err := database.SaveChannelSenderData("wecom-account", "wecom", route); err != nil {
		t.Fatal(err)
	}
	previous := channel.GetSenderFactory("wecom")
	defer channel.RegisterSenderFactory("wecom", previous)
	called := false
	channel.RegisterSenderFactory("wecom", func(ctx context.Context, data []byte, text string) error {
		called = true
		binding, ok := channel.NotificationBindingFromContext(ctx)
		if !ok || binding.AccountID != "account" || binding.Group != "devs" || string(data) != route || text != "result" {
			t.Fatalf("binding=%+v route=%s text=%s", binding, data, text)
		}
		return nil
	})
	if err := s.SendViaChannel(context.Background(), "result"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("saved factory not called")
	}
}

func TestNotificationResolutionUsesCurrentSelectionAndBinding(t *testing.T) {
	for _, channelType := range []string{"wechat", "wecom"} {
		t.Run(channelType, func(t *testing.T) {
			sess := NewSession("l1-session", "L1", &agent.Agent{Def: agent.Definition{NotifyChannel: "telegram", Channels: map[string]string{channelType: "old"}}}, nil, nil, nil)
			defer sess.Close()
			path := filepath.Join(t.TempDir(), "channels.yaml")
			sess.SetL1NotifyChannelPath(path)
			for _, account := range []string{"first", "second"} {
				if err := os.WriteFile(path, []byte("notify_channel: ' "+channelType+" '\nchannels:\n  "+channelType+": "+account+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				selected, binding, err := sess.resolveNotification(channelType)
				if err != nil || selected != channelType || binding.AccountID != account || binding.Group != "" {
					t.Fatalf("selection=%s binding=%+v err=%v", selected, binding, err)
				}
				called := false
				sess.SetChannelSender(channelType, func(ctx context.Context, _ string) error {
					got, ok := channel.NotificationBindingFromContext(ctx)
					if !ok || got != binding {
						t.Fatalf("send binding=%+v, want %+v", got, binding)
					}
					called = true
					return nil
				})
				if err := sess.SendViaChannel(context.Background(), "notification"); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("selected sender was not called")
				}
			}
			if err := os.WriteFile(path, []byte("notify_channel: ["), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := sess.resolveNotification(channelType)
			if err == nil || !strings.Contains(err.Error(), "parse notification channel config") {
				t.Fatalf("parse error=%v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			_, _, err = sess.resolveNotification(channelType)
			if err == nil || !strings.Contains(err.Error(), "read notification channel config") {
				t.Fatalf("read error=%v", err)
			}
		})
	}
}

func TestNotificationMalformedBindingsPreserveSelectionSemantics(t *testing.T) {
	for _, channelType := range []string{"wechat", "wecom", "qq", "telegram"} {
		t.Run(channelType, func(t *testing.T) {
			sess := NewSession("l1-session", "L1", &agent.Agent{}, nil, nil, nil)
			defer sess.Close()
			path := filepath.Join(t.TempDir(), "channels.yaml")
			if err := os.WriteFile(path, []byte("notify_channel: "+channelType+"\nchannels: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			sess.SetL1NotifyChannelPath(path)
			if !sess.HasNotifyChannel() {
				t.Error("malformed binding hid the configured notification channel")
			}
			called := false
			sess.SetChannelSender(channelType, func(context.Context, string) error { called = true; return nil })
			err := sess.SendViaChannel(context.Background(), "notification")
			if channelType == "wechat" || channelType == "wecom" {
				if err == nil || !strings.Contains(err.Error(), channelType+"_notification_config_invalid") {
					t.Errorf("text diagnostic=%v, want channel binding category", err)
				}
				if called {
					t.Error("bound sender called with invalid bindings")
				}
			} else if err != nil || !called {
				t.Errorf("unaffected channel delivery: called=%v err=%v", called, err)
			}
			mediaCalled := false
			sess.SetChannelMediaSender(channelType, func(context.Context, []channel.OutboundMedia) error { mediaCalled = true; return nil })
			err = sess.SendMediaViaChannel(context.Background(), []channel.OutboundMedia{{Kind: channel.MediaFile, Path: "unused"}})
			switch channelType {
			case "wecom":
				if err == nil || err.Error() != "wecom currently supports text notifications only" {
					t.Errorf("media diagnostic=%v", err)
				}
			case "wechat":
				if err == nil || !strings.Contains(err.Error(), "wechat_notification_config_invalid") {
					t.Errorf("media diagnostic=%v", err)
				}
			default:
				if err != nil || !mediaCalled {
					t.Errorf("unaffected media: called=%v err=%v", mediaCalled, err)
				}
			}
			if channelType == "wechat" {
				route, _, err := sess.NotificationRoute("wechat")
				if err == nil || !strings.Contains(err.Error(), "wechat_notification_config_invalid") || len(route) > 0 {
					t.Errorf("keepalive route=%s err=%v", route, err)
				}
			}
		})
	}
}
