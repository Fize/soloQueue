package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// AttachmentsDirName is the directory, inside a session's timeline directory,
// that holds the bytes of the attachments its messages reference. Keeping the
// bytes next to the rows that reference them means usage accounting, retention
// and deletion all act on one tree.
const AttachmentsDirName = "attachments"

// AttachmentsDir returns the attachment root of one session.
func AttachmentsDir(timelineDir string) string {
	if timelineDir == "" {
		return ""
	}
	return filepath.Join(timelineDir, AttachmentsDirName)
}

// SanitizeItemID makes a client-supplied item id usable as one directory name.
// Request ids arrive from the client, so anything outside a conservative set is
// replaced rather than trusted.
func SanitizeItemID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" || out == ".." {
		return ""
	}
	return out
}

// ItemAttachmentsDir returns the directory holding one message's attachments, or
// "" when the id cannot name a directory.
func ItemAttachmentsDir(timelineDir, itemID string) string {
	root := AttachmentsDir(timelineDir)
	id := SanitizeItemID(itemID)
	if root == "" || id == "" {
		return ""
	}
	return filepath.Join(root, id)
}

// AttachmentItemIDs lists the item ids whose rows carry attachments in events.
// Retention uses it to drop exactly the bytes that belong to the rows it
// removes.
func AttachmentItemIDs(events []timeline.Event) []string {
	var out []string
	for _, evt := range events {
		if evt.Message == nil || len(evt.Message.Files) == 0 {
			continue
		}
		if id := SanitizeItemID(evt.Message.ItemID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// PruneAttachments deletes attachment directories that keep does not name.
// Directories written within minAge of now are left alone: an upload is moved
// into place before its row is persisted, so a young directory can still belong
// to an in-flight message.
func PruneAttachments(timelineDir string, keep map[string]bool, minAge time.Duration, now time.Time) int {
	root := AttachmentsDir(timelineDir)
	if root == "" {
		return 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || keep[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if minAge > 0 && now.Sub(info.ModTime()) < minAge {
			continue
		}
		if os.RemoveAll(filepath.Join(root, entry.Name())) == nil {
			removed++
		}
	}
	return removed
}

// RemoveItemAttachments deletes one message's attachment directory.
func RemoveItemAttachments(timelineDir, itemID string) bool {
	dir := ItemAttachmentsDir(timelineDir, itemID)
	if dir == "" {
		return false
	}
	return os.RemoveAll(dir) == nil
}

// ClearAttachments deletes every attachment directory of one session.
func ClearAttachments(timelineDir string) bool {
	root := AttachmentsDir(timelineDir)
	if root == "" {
		return false
	}
	return os.RemoveAll(root) == nil
}
