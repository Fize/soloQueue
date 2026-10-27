package session

import "github.com/xiaobaitu/soloqueue/internal/channel"

// NotificationRoute reads opaque sender metadata only when channelType is the
// current notification selection. It only accesses this Session's TargetID;
// group routes must use L2SessionStore so reads and writes share the group lock.
func (s *Session) NotificationRoute(channelType string) ([]byte, channel.NotificationBinding, error) {
	selected, binding, err := s.resolveNotification(channelType)
	if err != nil || selected != channelType || s.closed.Load() {
		return nil, binding, err
	}
	s.channelSendersMu.RLock()
	defer s.channelSendersMu.RUnlock()
	if s.metaStore == nil {
		return nil, binding, nil
	}
	data, err := s.metaStore.GetChannelSenderData(s.TargetID, channelType)
	return []byte(data), binding, err
}

// UpdateNotificationRoute replaces only the exact metadata previously observed.
// The same lock guards this Session's inbound writes; group routes use the store
// API and its group lock. A mismatch leaves the current route intact.
func (s *Session) UpdateNotificationRoute(channelType string, before, after []byte) (bool, error) {
	s.channelSendersMu.Lock()
	defer s.channelSendersMu.Unlock()
	if s.metaStore == nil {
		return false, nil
	}
	return replaceNotificationRoute(s.metaStore, s.TargetID, channelType, before, after)
}

// GroupNotificationRoute reads opaque sender metadata for an inactive L2 group.
// The caller resolves that group's current notification selection.
func (s *L2SessionStore) GroupNotificationRoute(group, channelType string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.metaStore == nil {
		return nil, nil
	}
	data, err := s.metaStore.GetChannelSenderData(group, channelType)
	return []byte(data), err
}

// UpdateGroupNotificationRoute compares metadata under the group writer lock.
func (s *L2SessionStore) UpdateGroupNotificationRoute(group, channelType string, before, after []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metaStore == nil {
		return false, nil
	}
	return replaceNotificationRoute(s.metaStore, group, channelType, before, after)
}

func replaceNotificationRoute(store ChannelMetadataStore, target, channelType string, before, after []byte) (bool, error) {
	current, err := store.GetChannelSenderData(target, channelType)
	if err != nil {
		return false, err
	}
	if current != string(before) {
		return false, nil
	}
	if err := store.SaveChannelSenderData(target, channelType, string(after)); err != nil {
		return false, err
	}
	return true, nil
}
