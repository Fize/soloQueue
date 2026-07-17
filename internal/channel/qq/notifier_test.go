package qq

import (
	"context"
	"testing"
)

func TestQQNotifier_SendNotification_NilBridge(t *testing.T) {
	notifier := &QQNotifier{Bridge: nil}
	err := notifier.SendNotification(context.Background(), "u1", "c1", "test")
	if err == nil {
		t.Error("expected error when bridge is nil")
	}
}

func TestQQNotifier_ChatIDToSource_Short(t *testing.T) {
	got := chatIDToSource("abc123")
	want := int(SourceC2C)
	if got != want {
		t.Errorf("chatIDToSource(%q) = %d, want %d", "abc123", got, want)
	}
}

func TestQQNotifier_ChatIDToSource_Long(t *testing.T) {
	got := chatIDToSource("123456789012345678901") // 21 chars
	want := int(SourceGroup)
	if got != want {
		t.Errorf("chatIDToSource(long) = %d, want %d", got, want)
	}
}

func TestQQNotifier_ChatIDToSource_Empty(t *testing.T) {
	got := chatIDToSource("")
	want := int(SourceC2C)
	if got != want {
		t.Errorf("chatIDToSource(\"\") = %d, want %d", got, want)
	}
}
