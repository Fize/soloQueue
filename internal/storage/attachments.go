package storage

import (
	"os"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// Attachment housekeeping. Chat attachments live inside each session's timeline
// directory, so a row and its bytes are removed together by the operations that
// drop the row. These sweeps only converge leftovers: rows removed by retention,
// a send that never produced a row, or a draft that was abandoned.

const (
	// attachmentGrace keeps a young attachment directory: files are moved into
	// place before their row is persisted, so a fresh directory may belong to an
	// in-flight message.
	attachmentGrace = 30 * time.Minute
	// pendingAttachmentMaxAge bounds uploads that were never sent.
	pendingAttachmentMaxAge = 24 * time.Hour
	// pendingTimelineDirName mirrors the server's staging session key.
	pendingTimelineDirName = "_pending"
)

// sweepSessionAttachments drops attachment directories that no visible history
// references, plus uploads that were never sent.
func (m *Manager) sweepSessionAttachments(now time.Time) {
	timelinesRoot := filepath.Join(m.root, "logs", "timelines")
	entries, err := os.ReadDir(timelinesRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(timelinesRoot, entry.Name())
		attachments := transcript.AttachmentsDir(dir)
		if !hasChild(attachments) {
			continue
		}
		if entry.Name() == pendingTimelineDirName {
			pruneOldFiles(attachments, pendingAttachmentMaxAge, now)
			continue
		}
		transcript.PruneAttachments(dir, visibleItemIDs(dir), attachmentGrace, now)
	}
}

// visibleItemIDs returns the transcript items a session's history still shows.
// An unreadable timeline yields an empty set, which is why the caller applies a
// grace window: a pending write must never be mistaken for a deleted message.
func visibleItemIDs(timelineDir string) map[string]bool {
	events, err := timeline.ReadAllEvents(timelineDir)
	if err != nil {
		return map[string]bool{}
	}
	post, _ := transcript.PostClear(events)
	keep := map[string]bool{}
	for _, item := range transcript.Build(post, transcript.BuildOptions{}) {
		if id := transcript.SanitizeItemID(item.ID); id != "" {
			keep[id] = true
		}
	}
	return keep
}

// hasChild reports whether dir exists and holds at least one entry.
func hasChild(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// pruneOldFiles removes entries of dir older than maxAge, descending one level
// so per-item staging directories are emptied as a whole.
func pruneOldFiles(dir string, maxAge time.Duration, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < maxAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}
