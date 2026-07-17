package wechat

import (
	"context"
	"testing"
)

func TestWechatNotifier_SendNotification_NilClient(t *testing.T) {
	notifier := &WechatNotifier{Client: nil}
	err := notifier.SendNotification(context.Background(), "u1", "c1", "test")
	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestWechatNotifier_InterfaceCheck(t *testing.T) {
	// Compile-time check via var _ in notifier.go.
	// This test verifies the notifier type is constructable.
	_ = &WechatNotifier{}
}
