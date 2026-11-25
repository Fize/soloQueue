package transcript

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var (
	// ErrEpoch means the cursor belongs to an earlier journal instance (for
	// example before a process restart); the client must rebuild from a
	// snapshot.
	ErrEpoch = errors.New("transcript: epoch mismatch")
	// ErrLagged means frames after the cursor are no longer retained; the
	// client must rebuild from a snapshot.
	ErrLagged = errors.New("transcript: cursor outside retained window")
)

// Config bounds the memory retained by each Journal.
type Config struct {
	// MaxFrames and MaxBytes bound the replay ring. A cursor older than the
	// oldest retained frame must resynchronize from a snapshot.
	MaxFrames int
	MaxBytes  int
	// Grace keeps the items of an ended request in the materialized View long
	// enough for every persisted row to be readable by a concurrent snapshot.
	Grace time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// DefaultConfig returns production limits.
func DefaultConfig() Config {
	return Config{MaxFrames: 16384, MaxBytes: 16 << 20, Grace: 2 * time.Minute}
}

// Snapshot is a consistent copy of a Journal's in-memory state.
type Snapshot struct {
	Key      string
	Epoch    string
	Seq      uint64
	Items    []Item
	Pending  []PendingInput
	Requests []RequestState
}

// Journal is the ordered event log of one logical session.
type Journal struct {
	mu       sync.Mutex
	key      string
	epoch    string
	next     uint64
	frames   []Frame
	sizes    []int
	bytes    int
	view     *View
	openedAt map[string]time.Time
	endedAt  map[string]time.Time
	wake     chan struct{}
	cfg      Config
}

func newJournal(key string, firstSeq uint64, cfg Config) *Journal {
	if firstSeq == 0 {
		firstSeq = 1
	}
	if cfg.MaxFrames <= 0 {
		cfg.MaxFrames = DefaultConfig().MaxFrames
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultConfig().MaxBytes
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Journal{
		key:      key,
		epoch:    newEpoch(),
		next:     firstSeq,
		view:     NewView(),
		openedAt: make(map[string]time.Time),
		endedAt:  make(map[string]time.Time),
		wake:     make(chan struct{}),
		cfg:      cfg,
	}
}

func newEpoch() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(raw[:])
}

// Key returns the logical session identity.
func (j *Journal) Key() string { return j.key }

// Epoch identifies this journal instance.
func (j *Journal) Epoch() string { return j.epoch }

// Head returns the sequence of the last published frame (0 before the first).
func (j *Journal) Head() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.next - 1
}

// Wait returns a channel closed by the next Publish. Obtain it before reading
// so a frame published between the read and the wait is not missed.
func (j *Journal) Wait() <-chan struct{} {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.wake
}

// Publish assigns the next sequence and the server timestamp to f, applies it
// to the materialized View, appends it to the replay ring and wakes readers.
// It never blocks on readers.
func (j *Journal) Publish(f Frame) Frame {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.cfg.Now()
	f.Seq = j.next
	j.next++
	f.TS = now.Format(time.RFC3339Nano)
	for _, id := range j.view.Apply(f) {
		j.openedAt[id] = now
	}
	if f.Kind == KindRequestEnd {
		j.endedAt[f.RequestID] = now
		for _, id := range f.RequestIDs {
			j.endedAt[id] = now
		}
	}
	if f.Kind == KindTranscriptReset {
		for id := range j.openedAt {
			if _, ok := j.view.Items[id]; !ok {
				delete(j.openedAt, id)
			}
		}
	}
	size := frameSize(f)
	j.frames = append(j.frames, f)
	j.sizes = append(j.sizes, size)
	j.bytes += size
	for len(j.frames) > 1 && (len(j.frames) > j.cfg.MaxFrames || j.bytes > j.cfg.MaxBytes) {
		j.bytes -= j.sizes[0]
		j.frames = j.frames[1:]
		j.sizes = j.sizes[1:]
	}
	j.evictLocked(now)
	close(j.wake)
	j.wake = make(chan struct{})
	return f
}

func frameSize(f Frame) int {
	n := 160 + len(f.Text) + len(f.Args) + len(f.Result) + len(f.Error) + len(f.ItemID) + len(f.RequestID)
	for _, part := range f.Parts {
		n += len(part.Text) + 64
	}
	for _, seg := range f.Segments {
		n += len(seg.Text) + len(seg.Args) + len(seg.Result) + 64
	}
	return n
}

func (j *Journal) evictLocked(now time.Time) {
	if len(j.endedAt) == 0 {
		return
	}
	expired := make(map[string]bool)
	for id, at := range j.endedAt {
		if now.Sub(at) >= j.cfg.Grace {
			expired[id] = true
			delete(j.endedAt, id)
		}
	}
	if len(expired) == 0 {
		return
	}
	j.view.RemoveRequests(expired)
	for id := range j.openedAt {
		if _, ok := j.view.Items[id]; !ok {
			delete(j.openedAt, id)
		}
	}
}

// ItemOpened reports the sequence and time at which an item was opened. The
// persistence layer stamps both onto the timeline row of the item.
func (j *Journal) ItemOpened(itemID string) (uint64, time.Time, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	item, ok := j.view.Items[itemID]
	if !ok {
		return 0, time.Time{}, false
	}
	return item.Seq, j.openedAt[itemID], true
}

// ReadAfter returns up to max frames with a sequence greater than after. A
// different epoch returns ErrEpoch; a cursor outside the retained window
// (older than the ring, or ahead of the head) returns ErrLagged.
func (j *Journal) ReadAfter(epoch string, after uint64, max int) ([]Frame, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if epoch != j.epoch {
		return nil, ErrEpoch
	}
	head := j.next - 1
	if after > head {
		return nil, ErrLagged
	}
	if after == head {
		return nil, nil
	}
	if len(j.frames) == 0 || after+1 < j.frames[0].Seq {
		return nil, ErrLagged
	}
	start := int(after + 1 - j.frames[0].Seq)
	end := len(j.frames)
	if max > 0 && end-start > max {
		end = start + max
	}
	out := make([]Frame, end-start)
	copy(out, j.frames[start:end])
	return out, nil
}

// Snapshot copies the in-memory state together with the watermark. Every frame
// with a sequence at or below Seq is reflected in Items or in persisted rows
// readable after this call returns.
func (j *Journal) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{
		Key:      j.key,
		Epoch:    j.epoch,
		Seq:      j.next - 1,
		Items:    j.view.List(),
		Pending:  j.view.PendingList(),
		Requests: j.view.RequestList(),
	}
}
