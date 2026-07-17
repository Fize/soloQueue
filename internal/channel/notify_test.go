package channel

import (
	"context"
	"sync"
	"testing"
)

// mockNotifier implements ChannelNotifier for testing.
type mockNotifier struct {
	sentUser string
	sentConv string
	sentText string
	sendErr  error
	called   bool
}

func (m *mockNotifier) SendNotification(_ context.Context, userID, convID, text string) error {
	m.called = true
	m.sentUser = userID
	m.sentConv = convID
	m.sentText = text
	return m.sendErr
}

func newMockNotifier() *mockNotifier {
	return &mockNotifier{}
}

// ============== Registry ==============

func TestRegistry_RegisterAndFind(t *testing.T) {
	r := &Registry{}
	n := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: n})

	found, ok := r.Find("qq", "bot-a")
	if !ok {
		t.Fatal("Find returned false for registered entry")
	}
	if found != n {
		t.Error("Find returned wrong notifier")
	}
}

func TestRegistry_FindNonExistent(t *testing.T) {
	r := &Registry{}
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: newMockNotifier()})

	_, ok := r.Find("qq", "bot-b")
	if ok {
		t.Error("Find returned true for non-existent instance ID")
	}
}

func TestRegistry_FindExactMatch(t *testing.T) {
	r := &Registry{}
	nA := newMockNotifier()
	nB := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: nA})
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-b", Notifier: nB})

	found, ok := r.Find("qq", "bot-a")
	if !ok {
		t.Fatal("Find returned false")
	}
	if found != nA {
		t.Error("Find returned wrong notifier, expected bot-a")
	}
}

func TestRegistry_FindWrongType(t *testing.T) {
	r := &Registry{}
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: newMockNotifier()})

	_, ok := r.Find("wechat", "bot-a")
	if ok {
		t.Error("Find returned true for wrong channel type")
	}
}

func TestRegistry_HasAny_True(t *testing.T) {
	r := &Registry{}
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: newMockNotifier()})

	if !r.HasAny() {
		t.Error("HasAny returned false after register")
	}
}

func TestRegistry_HasAny_False(t *testing.T) {
	r := &Registry{}
	if r.HasAny() {
		t.Error("HasAny returned true for empty registry")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := &Registry{}
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: newMockNotifier()})
	r.Clear()

	if r.HasAny() {
		t.Error("HasAny returned true after Clear")
	}
	_, ok := r.Find("qq", "bot-a")
	if ok {
		t.Error("Find returned true after Clear")
	}
}

func TestRegistry_DoubleRegister(t *testing.T) {
	r := &Registry{}
	n1 := newMockNotifier()
	n2 := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: n1})
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: n2})

	found, ok := r.Find("qq", "bot-a")
	if !ok {
		t.Fatal("Find returned false")
	}
	if found != n2 {
		t.Error("Double register should replace with latest notifier")
	}
}

func TestRegistry_ConcurrentRegisterFind(t *testing.T) {
	r := &Registry{}
	var wg sync.WaitGroup

	// Register concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot", Notifier: newMockNotifier()})
		}()
	}

	// Find concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Find("qq", "bot")
		}()
	}

	wg.Wait()

	// Should not panic, and should find something
	_, ok := r.Find("qq", "bot")
	if !ok {
		t.Error("Find returned false after concurrent operations")
	}
}

// ============== NotifierForAgent ==============

func TestNotifierForAgent_NotifyChannelSet(t *testing.T) {
	r := &Registry{}
	nQQ := newMockNotifier()
	nWX := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: nQQ})
	r.Register(NotifierEntry{ChannelType: "wechat", InstanceID: "wx1", Notifier: nWX})

	channels := map[string]string{"qq": "bot-a", "wechat": "wx1"}
	notifier, ok := r.NotifierForAgent(channels, "qq")
	if !ok {
		t.Fatal("NotifierForAgent returned false")
	}
	if notifier != nQQ {
		t.Error("NotifierForAgent should return qq notifier when notify_channel=qq")
	}
}

func TestNotifierForAgent_NotifyChannelEmpty_UsesFirst(t *testing.T) {
	r := &Registry{}
	nQQ := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: nQQ})
	r.Register(NotifierEntry{ChannelType: "wechat", InstanceID: "wx1", Notifier: newMockNotifier()})

	// Empty notifyChannel should use whatever map iteration returns first
	channels := map[string]string{"qq": "bot-a", "wechat": "wx1"}
	notifier, ok := r.NotifierForAgent(channels, "")
	if !ok {
		t.Fatal("NotifierForAgent returned false with empty notifyChannel")
	}
	if notifier == nil {
		t.Error("NotifierForAgent returned nil notifier")
	}
}

func TestNotifierForAgent_NotifyChannelNotInChannels_Fallback(t *testing.T) {
	r := &Registry{}
	nQQ := newMockNotifier()
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: nQQ})

	channels := map[string]string{"qq": "bot-a"}
	notifier, ok := r.NotifierForAgent(channels, "wechat") // wechat not in channels
	if !ok {
		t.Fatal("NotifierForAgent should fallback to first channel")
	}
	if notifier != nQQ {
		t.Error("NotifierForAgent should fallback to qq notifier")
	}
}

func TestNotifierForAgent_NoChannels(t *testing.T) {
	r := &Registry{}
	r.Register(NotifierEntry{ChannelType: "qq", InstanceID: "bot-a", Notifier: newMockNotifier()})

	_, ok := r.NotifierForAgent(nil, "")
	if ok {
		t.Error("NotifierForAgent returned true for nil channels")
	}
	_, ok = r.NotifierForAgent(map[string]string{}, "")
	if ok {
		t.Error("NotifierForAgent returned true for empty channels")
	}
}
