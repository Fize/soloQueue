package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// This file derives a session's activity timestamps from the timeline files
// that belong to it.
//
// The directory's own modification time must never be used for ordering: meta
// writes go through a `meta.json.tmp.<pid>` sibling plus rename (see
// MergeAndSavePath), so every metadata write adds and removes a directory
// entry and bumps the directory mtime. Using it as a sort key made the session
// list reorder itself just by being listed. Only the timeline files carry
// conversation activity.

// timelineActivityFiles returns the non-empty timeline files of a session
// directory. Empty files are skipped because rotation creates the next
// day/segment file before writing into it.
func timelineActivityFiles(dir string) []string {
	files, err := timeline.ListTimelineFiles(dir, "timeline")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		info, statErr := os.Stat(f)
		if statErr != nil || info.Size() == 0 {
			continue
		}
		out = append(out, f)
	}
	return out
}

// LatestTimelineWrite returns the modification time of the most recently
// written timeline file of a session. It is the wall-clock moment of the last
// conversation activity, and is zero when the session has no timeline content
// yet.
func LatestTimelineWrite(dir string) time.Time {
	var latest time.Time
	for _, f := range timelineActivityFiles(dir) {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest.UTC()
}

// FirstEventTime returns the timestamp of the first timeline event ever
// recorded for a session. It reads a single line from the earliest timeline
// file and falls back to that file's modification time when the first line is
// unreadable or carries no timestamp.
func FirstEventTime(dir string) time.Time {
	files := timelineActivityFiles(dir)
	if len(files) == 0 {
		return time.Time{}
	}

	// Files are appended chronologically, so the file with the smallest mtime
	// holds the earliest event.
	oldest := files[0]
	oldestMod := time.Time{}
	if info, err := os.Stat(oldest); err == nil {
		oldestMod = info.ModTime()
	}
	for _, f := range files[1:] {
		info, err := os.Stat(f)
		if err != nil || !info.ModTime().Before(oldestMod) {
			continue
		}
		oldest = f
		oldestMod = info.ModTime()
	}

	if ts, ok := firstEventTimestamp(oldest); ok {
		return ts
	}
	if !oldestMod.IsZero() {
		return oldestMod.UTC()
	}
	return time.Time{}
}

// firstEventTimestamp scans the head of a timeline file for the first event
// that carries a parseable timestamp.
func firstEventTimestamp(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var ev struct {
			Timestamp string `json:"ts"`
		}
		if json.Unmarshal(scanner.Bytes(), &ev) != nil || ev.Timestamp == "" {
			continue
		}
		ts, perr := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if perr != nil {
			continue
		}
		return ts.UTC(), true
	}
	return time.Time{}, false
}

// SessionMetadataWrite returns the modification time of a session's meta.json.
// It is the last-resort timestamp for a session whose timeline is still empty
// (a chat whose first message never reached the log), so a real moment is
// reported instead of a zero timestamp. It is never preferred over timeline
// activity: metadata writes also happen for reasons unrelated to conversation.
func SessionMetadataWrite(dir string) time.Time {
	info, err := os.Stat(filepath.Join(dir, MetaFileName))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

// latestActivity picks the newest of the given timestamps.
func latestActivity(times ...time.Time) time.Time {
	var latest time.Time
	for _, t := range times {
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

// timelineDirFor returns the timeline directory of an L2 session.
func timelineDirFor(workDir, id string) string {
	return filepath.Join(workDir, "logs", "timelines", "l2-"+id)
}
