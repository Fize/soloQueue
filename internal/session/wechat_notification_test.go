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
