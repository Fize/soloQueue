package channel

import (
	"context"
	"sync"
)

// ChannelNotifier sends notification text through a specific channel instance.
// Each channel type (QQ, WeChat) implements this interface to provide
// channel-specific notification delivery for cron task completions.
type ChannelNotifier interface {
	// SendNotification sends a text notification to the given user/conversation.
	// userID and conversationID come from the task's SourceUserID / SourceConvID.
	SendNotification(ctx context.Context, userID, conversationID, text string) error
}

// NotifierEntry binds a ChannelNotifier to its channel type and instance ID.
type NotifierEntry struct {
	ChannelType string           // "qq" | "wechat"
	InstanceID  string           // e.g. "my-qq-bot", "default"
	Notifier    ChannelNotifier
}

// Registry tracks all active channel notifiers for cron notification routing.
// Thread-safe. Hot-reload calls Clear() then re-registers all channels.
type Registry struct {
	mu      sync.RWMutex
	entries []NotifierEntry
}

// Register adds or replaces a notifier entry. If an entry with the same
// (ChannelType, InstanceID) already exists, it is replaced.
func (r *Registry) Register(entry NotifierEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.ChannelType == entry.ChannelType && e.InstanceID == entry.InstanceID {
			r.entries[i] = entry
			return
		}
	}
	r.entries = append(r.entries, entry)
}

// Find looks up a ChannelNotifier by channel type and instance ID.
// Returns (notifier, true) if found, (nil, false) otherwise.
func (r *Registry) Find(channelType, instanceID string) (ChannelNotifier, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.ChannelType == channelType && e.InstanceID == instanceID {
			return e.Notifier, true
		}
	}
	return nil, false
}

// HasAny reports whether any notifier entries are registered at all.
func (r *Registry) HasAny() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries) > 0
}

// Clear removes all registered entries. Called before a full hot-reload.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

// Ensure Registry implements no extra interfaces.
var _ = (*Registry)(nil)
