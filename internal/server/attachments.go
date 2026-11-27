package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// pendingTimelineDirName holds uploads that are not bound to a session yet
// (a new chat whose real id does not exist). The leading underscore keeps
// handleListSessions, which only accepts l2- prefixed entries, from treating it
// as a session.
const pendingTimelineDirName = "_pending"

// uploadTimelineDir resolves where a session's uploads land: the session's own
// timeline directory, so bytes and the rows referencing them share one tree.
func (m *Mux) uploadTimelineDir(sessionID string) string {
	if id, ok := strings.CutPrefix(sessionID, "l2:"); ok && id != "" {
		return filepath.Join(m.workDir, "logs", "timelines", "l2-"+id)
	}
	if sessionID == "" || sessionID == "l1" || sessionID == "default" {
		return filepath.Join(m.workDir, "logs", "timelines", "default")
	}
	return filepath.Join(m.workDir, "logs", "timelines", pendingTimelineDirName)
}

// uploadAttachmentsDir is the attachment root an upload is written to before it
// is bound to a message item.
func (m *Mux) uploadAttachmentsDir(sessionID string) string {
	return transcript.AttachmentsDir(m.uploadTimelineDir(sessionID))
}

// moveFilesIntoItem binds uploaded files to the message that references them:
// every file moves into <session>/attachments/<item-id>/ and the returned refs
// carry the new paths. Failures keep the original path so a send never breaks.
func (m *Mux) moveFilesIntoItem(sessionID, requestID string, files []ClientFile) []ClientFile {
	if len(files) == 0 || requestID == "" {
		return files
	}
	timelineDir := m.uploadTimelineDir(sessionID)
	itemDir := transcript.ItemAttachmentsDir(timelineDir, transcript.InputItemID(requestID))
	if itemDir == "" {
		return files
	}
	out := make([]ClientFile, len(files))
	copy(out, files)
	for i, f := range out {
		path := strings.TrimSpace(f.Path)
		if path == "" || filepath.Dir(filepath.Clean(path)) == itemDir {
			continue
		}
		if err := os.MkdirAll(itemDir, 0o755); err != nil {
			continue
		}
		dest := uniquePath(itemDir, filepath.Base(path))
		if err := os.Rename(path, dest); err != nil {
			continue
		}
		out[i].Path = dest
	}
	return out
}

// uniquePath returns a path inside dir that no file occupies yet.
func uniquePath(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err != nil {
		return dest
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 1; n < 1000; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, n, ext))
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, os.Getpid(), ext))
}
