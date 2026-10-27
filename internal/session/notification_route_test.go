package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func TestNotificationRouteOpaqueConditionalUpdate(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "route.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, channelType := range []string{"wechat", "telegram"} {
		t.Run(channelType, func(t *testing.T) {
			sess := NewSession("l1-session", "L1", &agent.Agent{Def: agent.Definition{NotifyChannel: channelType, Channels: map[string]string{channelType: "account"}}}, nil, nil, nil)
			defer sess.Close()
			sess.metaStore = database
			groups := NewL2SessionStore(nil, t.TempDir(), sess.logger)
			groups.SetChannelMetadataStore(database)
			for _, group := range []bool{false, true} {
				before := []byte("opaque route, no JSON required")
				after := []byte("opaque replacement")
				newest := []byte("opaque replacement ")
				var update func([]byte, []byte) (bool, error)
				var load func() ([]byte, error)
				if group {
					groups.SetChannelSenderDataForGroup("research", channelType, before, func(context.Context, string) error { return nil })
					update = func(before, after []byte) (bool, error) {
						return groups.UpdateGroupNotificationRoute("research", channelType, before, after)
					}
					load = func() ([]byte, error) { return groups.GroupNotificationRoute("research", channelType) }
				} else {
					sess.SetChannelSenderData(channelType, before, func(context.Context, string) error { return nil })
					update = func(before, after []byte) (bool, error) {
						return sess.UpdateNotificationRoute(channelType, before, after)
					}
					load = func() ([]byte, error) {
						data, binding, err := sess.NotificationRoute(channelType)
						if binding.AccountID != "account" {
							t.Errorf("binding=%+v", binding)
						}
						return data, err
					}
				}
				if got, err := load(); err != nil || string(got) != string(before) {
					t.Fatalf("route=%q err=%v", got, err)
				}
				if updated, err := update(before, after); err != nil || !updated {
					t.Fatalf("matching update=%v err=%v", updated, err)
				}
				if group {
					groups.SetChannelSenderDataForGroup("research", channelType, newest, func(context.Context, string) error { return nil })
				} else {
					sess.SetChannelSenderData(channelType, newest, func(context.Context, string) error { return nil })
				}
				if updated, err := update(after, []byte("stale result")); err != nil || updated {
					t.Fatalf("stale update=%v err=%v", updated, err)
				}
				if got, err := load(); err != nil || string(got) != string(newest) {
					t.Fatalf("new inbound route=%q err=%v", got, err)
				}
			}
		})
	}
}

func TestNotificationRouteUpdateReportsMissingStoreAndReadFailure(t *testing.T) {
	sess := NewSession("l1-session", "L1", &agent.Agent{}, nil, nil, nil)
	defer sess.Close()
	if updated, err := sess.UpdateNotificationRoute("telegram", []byte("old"), []byte("new")); updated || err != nil {
		t.Fatalf("missing store updated=%v err=%v", updated, err)
	}
	sess.metaStore = failedWechatRouteStore{}
	if updated, err := sess.UpdateNotificationRoute("telegram", []byte("old"), []byte("new")); updated || err == nil {
		t.Fatalf("failed read updated=%v err=%v", updated, err)
	}
	groups := NewL2SessionStore(nil, t.TempDir(), sess.logger)
	if updated, err := groups.UpdateGroupNotificationRoute("research", "telegram", []byte("old"), []byte("new")); updated || err != nil {
		t.Fatalf("missing group store updated=%v err=%v", updated, err)
	}
	groups.SetChannelMetadataStore(failedWechatRouteStore{})
	if updated, err := groups.UpdateGroupNotificationRoute("research", "telegram", []byte("old"), []byte("new")); updated || err == nil {
		t.Fatalf("failed group read updated=%v err=%v", updated, err)
	}
}

func TestNotificationRouteSessionAPICannotAccessAppliedGroupRoute(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sess := NewSession("temporary-task", "research", &agent.Agent{Def: agent.Definition{NotifyChannel: "wechat", Channels: map[string]string{"wechat": "account"}}}, nil, nil, nil)
	defer sess.Close()
	groups := NewL2SessionStore(nil, t.TempDir(), sess.logger)
	groups.SetChannelMetadataStore(database)
	original, newest := []byte("group-original"), []byte("group-newest")
	groups.SetChannelSenderDataForGroup("research", "wechat", original, func(context.Context, string) error { return nil })
	groups.ApplyChannelSendersTo("research", sess)
	groups.SetChannelSenderDataForGroup("research", "wechat", newest, func(context.Context, string) error { return nil })
	if data, _, err := sess.NotificationRoute("wechat"); err != nil || len(data) > 0 {
		t.Fatalf("Session route accessed group metadata: %q err=%v", data, err)
	}
	if updated, err := sess.UpdateNotificationRoute("wechat", newest, []byte("stale-session-update")); err != nil || updated {
		t.Fatalf("Session group update=%v err=%v", updated, err)
	}
	if data, err := groups.GroupNotificationRoute("research", "wechat"); err != nil || string(data) != string(newest) {
		t.Fatalf("new group metadata changed: %q err=%v", data, err)
	}
	// The generic Session route API still works on its own target, separately.
	own := []byte("session-owned")
	sess.SetChannelSenderData("wechat", own, func(context.Context, string) error { return nil })
	if data, _, err := sess.NotificationRoute("wechat"); err != nil || string(data) != string(own) {
		t.Fatalf("own Session metadata=%q err=%v", data, err)
	}
	if updated, err := sess.UpdateNotificationRoute("wechat", own, []byte("own-update")); err != nil || !updated {
		t.Fatalf("own update=%v err=%v", updated, err)
	}
	if data, err := groups.GroupNotificationRoute("research", "wechat"); err != nil || string(data) != string(newest) {
		t.Fatalf("own update changed group: %q err=%v", data, err)
	}
	// Existing notification delivery retains its separate group routing behavior.
	sess.channelSendersMu.Lock()
	delete(sess.channelSenders, "wechat")
	sess.channelSendersMu.Unlock()
	previous := channel.GetSenderFactory("wechat")
	defer channel.RegisterSenderFactory("wechat", previous)
	called := false
	channel.RegisterSenderFactory("wechat", func(ctx context.Context, data []byte, text string) error {
		if string(data) != string(newest) || text != "notification" {
			t.Fatalf("delivery route=%q text=%q", data, text)
		}
		binding, ok := channel.NotificationBindingFromContext(ctx)
		if !ok || binding.Group != "research" || binding.AccountID != "account" {
			t.Fatalf("delivery binding=%+v", binding)
		}
		called = true
		return nil
	})
	if err := sess.SendViaChannel(context.Background(), "notification"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("group notification delivery was skipped")
	}
}
