package session

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// timelineDir returns the session's timeline directory, or "" when the session
// is not persisted.
func (s *Session) timelineDir() string {
	if s.tl == nil {
		return ""
	}
	return s.tl.Dir()
}

// dropAttachments removes the attachment directories of the given transcript
// items. Attachments live inside the session's timeline directory, so a dropped
// row and its bytes are removed in one place.
func (s *Session) dropAttachments(itemIDs []string) {
	dir := s.timelineDir()
	if dir == "" || len(itemIDs) == 0 {
		return
	}
	for _, id := range itemIDs {
		transcript.RemoveItemAttachments(dir, id)
	}
}

// clearAttachments removes every attachment directory of this session.
func (s *Session) clearAttachments() {
	dir := s.timelineDir()
	if dir == "" {
		return
	}
	transcript.ClearAttachments(dir)
}

// itemIDsDroppedByRewind lists the items Rewind will discard, excluding the
// boundary message: the composer refills that message and re-sends it, so its
// attachments must outlive the rewind.
func (s *Session) itemIDsDroppedByRewind(ts time.Time) []string {
	if s.cw == nil {
		return nil
	}
	var ids []string
	for i := 0; i < s.cw.Len(); i++ {
		msg, ok := s.cw.MessageAt(i)
		if !ok || msg.ItemID == "" || msg.Timestamp.IsZero() {
			continue
		}
		if msg.Timestamp.After(ts) {
			ids = append(ids, msg.ItemID)
		}
	}
	return ids
}

// itemIDsAt lists the items whose timestamp is in the deletion set, mirroring
// ContextWindow.DeleteMessages.
func (s *Session) itemIDsAt(targets []time.Time) []string {
	if s.cw == nil {
		return nil
	}
	set := make(map[time.Time]bool, len(targets))
	for _, ts := range targets {
		set[ts] = true
	}
	var ids []string
	for i := 0; i < s.cw.Len(); i++ {
		msg, ok := s.cw.MessageAt(i)
		if !ok || msg.ItemID == "" || msg.Timestamp.IsZero() {
			continue
		}
		if set[msg.Timestamp] {
			ids = append(ids, msg.ItemID)
		}
	}
	return ids
}
