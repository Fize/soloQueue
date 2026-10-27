package dispatch

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var fileMu sync.Mutex

// ReadEvents reads the existing dispatch journal; it does not instantiate/reconcile a live Manager.
func ReadEvents(dir string) ([]Event, error) {
	return readEvents(dir)
}

// ReadEventsForRecord binds optional detail reads to the authorized metadata directory and identity.
func ReadEventsForRecord(dir string, record Record) ([]Event, error) {
	events, err := readEvents(dir)
	if err != nil {
		return nil, err
	}
	for _, ev := range events {
		if !sameEventIdentity(ev.Record, record) {
			return nil, errors.New("dispatch event identity mismatch")
		}
	}
	return events, nil
}

// CompactCompletedDetails preserves the authoritative terminal record before dropping optional event bodies.
// A terminal dispatch never accepts new progress, and fileMu serializes with commit writes.
type CompactionStats struct {
	DeletedFiles int
	FreedBytes   int64
}

func CompactCompletedDetails(dir string) error {
	_, err := CompactCompletedDetailsWithStats(dir)
	return err
}
func CompactCompletedDetailsWithStats(dir string) (stats CompactionStats, retErr error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if !strings.HasPrefix(filepath.Base(dir), "dlg_") {
		return stats, errors.New("invalid dispatch directory")
	}
	_, beforeBytes, err := dispatchDirectorySize(dir)
	if err != nil {
		return stats, err
	}
	defer func() {
		_, afterBytes, err := dispatchDirectorySize(dir)
		if err != nil {
			retErr = errors.Join(retErr, err)
			return
		}
		stats.FreedBytes = beforeBytes - afterBytes
	}()
	latest, e := readLatestEventUnlocked(dir)
	if e != nil {
		return stats, e
	}
	if latest == nil {
		return stats, nil
	}
	last := *latest
	if last.Record.Status != StatusCompleted && last.Record.Status != StatusFailed && last.Record.Status != StatusInterrupted {
		return stats, errors.New("dispatch active")
	}
	if last.Record.DetailsExpired {
		return stats, nil
	}
	last.Record.DetailsExpired = true
	last.Record.Revision++
	last.Payload = nil
	last.Type = "details_expired"
	last.Sequence = last.Record.Revision
	paths, e := filepath.Glob(filepath.Join(dir, "stream-*.jsonl"))
	if e != nil || len(paths) == 0 {
		return stats, e
	}
	target := paths[len(paths)-1]
	b, e := json.Marshal(last)
	if e != nil {
		return stats, e
	}
	f, e := os.CreateTemp(dir, ".compact-")
	if e != nil {
		return stats, e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(append(b, '\n'))
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return stats, e
	}
	if e = os.Rename(name, target); e != nil {
		return stats, e
	}
	if e = writeAtomicJSON(filepath.Join(dir, "meta.json"), last.Record); e != nil {
		return stats, e
	}
	for _, p := range paths {
		if p != target {
			if e = os.Remove(p); e != nil {
				return stats, e
			}
			stats.DeletedFiles++
		}
	}
	return stats, nil
}

func dispatchDirectorySize(dir string) (int, int64, error) {
	root, err := openDispatchRoot(dir)
	if err != nil {
		return 0, 0, err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return 0, 0, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maxDispatchEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	if len(entries) > maxDispatchEntries {
		return 0, 0, ErrReadLimit
	}
	var count int
	var bytes int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return 0, 0, err
		}
		if info.Mode().IsRegular() {
			count++
			bytes += info.Size()
		}
	}
	return count, bytes, nil
}
