package dispatch

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Read limits apply to old journals as well as files written under current quotas.
const (
	maxDispatchRecordBytes   = 16 << 20
	maxDispatchPageBytes     = 32 << 20
	maxDispatchHistoryBytes  = 64 << 20
	maxDispatchEntries       = 4096
	maxDispatchHistoryEvents = 100000
)

var ErrReadLimit = errors.New("dispatch detail exceeds read limit")

type EventPage struct {
	Events     []Event
	NextCursor string
}
type eventCursor struct {
	File     string `json:"f"`
	Offset   int64  `json:"o"`
	Sequence uint64 `json:"s"`
}

// ReadEventPageForRecord reads one bounded page. Cursors are opaque; existing numeric
// cursors remain accepted within the bounded legacy scan allowance.
func ReadEventPageForRecord(dir string, record Record, cursor string, limit int) (EventPage, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return readEventPageUnlocked(dir, &record, cursor, limit, maxDispatchPageBytes)
}

func openDispatchRoot(dir string) (*os.Root, error) {
	base := filepath.Dir(filepath.Dir(dir))
	for p := filepath.Clean(dir); p != base; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe dispatch directory")
		}
	}
	return os.OpenRoot(dir)
}
func openDispatchFile(root *os.Root, name string) (*os.File, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("invalid dispatch file")
	}
	return root.Open(name)
}
func dispatchFiles(root *os.Root) ([]string, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maxDispatchEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxDispatchEntries {
		return nil, ErrReadLimit
	}
	paths := []string{}
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), "stream-") && strings.HasSuffix(en.Name(), ".jsonl") {
			paths = append(paths, en.Name())
		}
	}
	sort.Strings(paths)
	return paths, nil
}
func encodeEventCursor(c eventCursor) string {
	b, _ := json.Marshal(c)
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}

func readEventPageUnlocked(dir string, expected *Record, cursor string, limit int, byteBudget int64) (EventPage, error) {
	page := EventPage{Events: []Event{}}
	root, err := openDispatchRoot(dir)
	if err != nil {
		return page, err
	}
	defer root.Close()
	files, err := dispatchFiles(root)
	if err != nil {
		return page, err
	}
	current := eventCursor{}
	skip := 0
	if cursor != "" {
		if strings.HasPrefix(cursor, "v1.") {
			if len(cursor) > 1024 {
				return page, errors.New("invalid dispatch cursor")
			}
			b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "v1."))
			if e != nil {
				return page, errors.New("invalid dispatch cursor")
			}
			if json.Unmarshal(b, &current) != nil || current.Offset < 0 || filepath.Base(current.File) != current.File {
				return page, errors.New("invalid dispatch cursor")
			}
		} else {
			skip, err = strconv.Atoi(cursor)
			if err != nil || skip < 0 || skip > maxDispatchHistoryEvents {
				return page, errors.New("invalid dispatch cursor")
			}
		}
	}
	start := 0
	if current.File != "" {
		start = sort.SearchStrings(files, current.File)
		if start == len(files) || files[start] != current.File {
			return page, errors.New("dispatch cursor is stale")
		}
	}
	var identity *Record = expected
	accept := func(event Event) error {
		if event.Record.ID != filepath.Base(dir) || event.Sequence == 0 {
			return errors.New("invalid dispatch event")
		}
		if identity == nil {
			r := event.Record
			identity = &r
		}
		if !sameEventIdentity(event.Record, *identity) {
			return errors.New("dispatch event identity mismatch")
		}
		return nil
	}
	var scanned int64
	for i := start; i < len(files); i++ {
		f, e := openDispatchFile(root, files[i])
		if e != nil {
			return page, e
		}
		offset := int64(0)
		if i == start {
			offset = current.Offset
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return page, e
		}
		if offset > st.Size() {
			f.Close()
			return page, errors.New("dispatch cursor is stale")
		}
		if _, e = f.Seek(offset, io.SeekStart); e != nil {
			f.Close()
			return page, e
		}
		// A scanner bounds each JSONL record before json.Unmarshal can allocate its body.
		scanner := bufio.NewScanner(io.LimitReader(f, byteBudget-scanned+maxDispatchRecordBytes))
		scanner.Buffer(make([]byte, 4096), maxDispatchRecordBytes)
		scanner.Split(splitDispatchLine)
		for scanner.Scan() {
			line := scanner.Bytes()
			consumed := int64(len(line))
			if scanned+consumed > byteBudget {
				f.Close()
				if len(page.Events) > 0 {
					page.NextCursor = encodeEventCursor(eventCursor{File: files[i], Offset: offset, Sequence: current.Sequence})
					return page, nil
				}
				return page, ErrReadLimit
			}
			offset += consumed
			scanned += consumed
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var event Event
			if e = json.Unmarshal(line, &event); e != nil {
				f.Close()
				return page, fmt.Errorf("dispatch: decode journal: %w", e)
			}
			if e = accept(event); e != nil {
				f.Close()
				return page, e
			}
			current.Sequence = event.Sequence
			if skip > 0 {
				skip--
				continue
			}
			page.Events = append(page.Events, event)
			if len(page.Events) >= limit {
				page.NextCursor = encodeEventCursor(eventCursor{File: files[i], Offset: offset, Sequence: current.Sequence})
				f.Close()
				return page, nil
			}
		}
		current.File = files[i]
		current.Offset = offset
		e = scanner.Err()
		f.Close()
		if e != nil {
			return page, fmt.Errorf("%w: journal record", ErrReadLimit)
		}
	}
	f, e := openDispatchFile(root, "state.json")
	if os.IsNotExist(e) {
		return page, nil
	}
	if e != nil {
		return page, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return page, e
	}
	if st.Size() > maxDispatchRecordBytes {
		return page, fmt.Errorf("%w: checkpoint", ErrReadLimit)
	}
	if st.Size() > byteBudget-scanned {
		if len(page.Events) > 0 {
			page.NextCursor = encodeEventCursor(current)
			return page, nil
		}
		return page, fmt.Errorf("%w: checkpoint", ErrReadLimit)
	}
	data, e := io.ReadAll(io.LimitReader(f, maxDispatchRecordBytes+1))
	if e != nil {
		return page, e
	}
	if len(data) > maxDispatchRecordBytes {
		return page, ErrReadLimit
	}
	var checkpoint Event
	if e = json.Unmarshal(data, &checkpoint); e != nil {
		return page, e
	}
	if e = accept(checkpoint); e != nil {
		return page, e
	}
	if checkpoint.Sequence > current.Sequence && skip == 0 {
		page.Events = append(page.Events, checkpoint)
	}
	return page, nil
}

// Keep the delimiter in the token so cursor offsets remain exact, including CRLF.
func splitDispatchLine(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i+1], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Recovery needs the latest full record, not every optional progress body. Read a
// bounded tail of the newest nonempty journal, then reconcile its checkpoint.
func readLatestEvent(dir string) (*Event, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return readLatestEventUnlocked(dir)
}
func readLatestEventUnlocked(dir string) (*Event, error) {
	root, err := openDispatchRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files, err := dispatchFiles(root)
	if err != nil {
		return nil, err
	}
	var last *Event
	var scanned int64
	for i := len(files) - 1; i >= 0; i-- {
		f, err := openDispatchFile(root, files[i])
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if st.Size() == 0 {
			f.Close()
			continue
		}
		start := max(int64(0), st.Size()-maxDispatchRecordBytes-1)
		readBytes := st.Size() - start
		if readBytes > maxDispatchHistoryBytes-scanned {
			f.Close()
			return nil, ErrReadLimit
		}
		scanned += readBytes
		data := make([]byte, readBytes)
		_, err = f.ReadAt(data, start)
		f.Close()
		if err != nil {
			return nil, err
		}
		end := len(data)
		for end > 0 && (data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == ' ' || data[end-1] == '\t') {
			end--
		}
		if end == 0 {
			if start > 0 {
				return nil, ErrReadLimit
			}
			continue
		}
		begin := end
		for begin > 0 && data[begin-1] != '\n' {
			begin--
		}
		if begin == 0 && start > 0 || end-begin > maxDispatchRecordBytes {
			return nil, ErrReadLimit
		}
		var event Event
		if err = json.Unmarshal(data[begin:end], &event); err != nil {
			return nil, err
		}
		if event.Record.ID != filepath.Base(dir) || event.Sequence == 0 {
			return nil, errors.New("invalid dispatch event")
		}
		last = &event
		break
	}
	f, err := openDispatchFile(root, "state.json")
	if os.IsNotExist(err) {
		return last, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > maxDispatchRecordBytes || st.Size() > maxDispatchHistoryBytes-scanned {
		return nil, ErrReadLimit
	}
	data, err := io.ReadAll(io.LimitReader(f, min(maxDispatchRecordBytes+1, maxDispatchHistoryBytes-scanned+1)))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDispatchRecordBytes || int64(len(data)) > maxDispatchHistoryBytes-scanned {
		return nil, ErrReadLimit
	}
	var checkpoint Event
	if err = json.Unmarshal(data, &checkpoint); err != nil {
		return nil, err
	}
	if checkpoint.Record.ID != filepath.Base(dir) || checkpoint.Sequence == 0 || last != nil && !sameEventIdentity(last.Record, checkpoint.Record) {
		return nil, errors.New("invalid dispatch checkpoint")
	}
	if last == nil || checkpoint.Sequence > last.Sequence {
		return &checkpoint, nil
	}
	return last, nil
}
