package session

import (
	"context"
	"errors"
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
