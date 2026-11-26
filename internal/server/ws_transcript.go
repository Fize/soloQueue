package server

import (
	"encoding/json"
	"errors"

	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// maxFramesPerMessage bounds one batched WebSocket message.
const maxFramesPerMessage = 512

// maxFramesBytes bounds how much one batched message may carry. Frame counts
// alone do not bound it: a single snapshot or tool result can be megabytes, and
// the client parses and renders every byte it receives. A frame that alone
// exceeds the budget is still sent whole, because frames must stay contiguous.
const maxFramesBytes = 256 << 10

// framesMessage carries consecutive journal frames of one session.
type framesMessage struct {
	Type      string             `json:"type"`
	SessionID string             `json:"session_id"`
	Epoch     string             `json:"epoch"`
	Frames    []transcript.Frame `json:"frames"`
}

// resyncMessage tells the client its cursor cannot be served; it must load a
// snapshot and subscribe again after its watermark.
type resyncMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Epoch     string `json:"epoch"`
	HeadSeq   uint64 `json:"head_seq"`
	Reason    string `json:"reason"`
}

// subscription is one client's cursor into one session journal. Only the
// client's write goroutine reads or advances the cursor.
type subscription struct {
	key     string
	journal *transcript.Journal
	epoch   string
	cursor  uint64
	stalled bool // a resync was requested; wait for a new subscribe
	stop    chan struct{}
}

// poke wakes the write goroutine without blocking.
func (c *Client) poke() {
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

// subscribe starts (or replaces) the client's cursor for a session. An empty
// epoch subscribes from the current head; otherwise the cursor resumes after
// afterSeq of that epoch and is validated by the next flush.
func (c *Client) subscribe(key string, j *transcript.Journal, epoch string, afterSeq uint64) {
	if epoch == "" {
		epoch = j.Epoch()
		afterSeq = j.Head()
	}
	sub := &subscription{key: key, journal: j, epoch: epoch, cursor: afterSeq, stop: make(chan struct{})}
	c.subsMu.Lock()
	if old, ok := c.subs[key]; ok {
		close(old.stop)
	}
	c.subs[key] = sub
	c.subsMu.Unlock()
	go c.watch(sub)
	c.poke()
}

func (c *Client) unsubscribe(key string) {
	c.subsMu.Lock()
	if sub, ok := c.subs[key]; ok {
		close(sub.stop)
		delete(c.subs, key)
	}
	c.subsMu.Unlock()
}

func (c *Client) closeSubscriptions() {
	c.subsMu.Lock()
	for key, sub := range c.subs {
		close(sub.stop)
		delete(c.subs, key)
	}
	c.subsMu.Unlock()
}

// watch wakes the writer after every publish to the subscribed journal.
func (c *Client) watch(sub *subscription) {
	for {
		wake := sub.journal.Wait()
		select {
		case <-wake:
			c.poke()
		case <-sub.stop:
			return
		case <-c.ctx.Done():
			return
		}
	}
}

// pendingTranscriptMessages reads every subscription's frames after its
// cursor and returns the encoded messages to write. It runs only on the
// write goroutine. more reports that a subscription still has frames queued.
func (c *Client) pendingTranscriptMessages() (messages [][]byte, more bool) {
	c.subsMu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.subsMu.Unlock()
	for _, sub := range subs {
		if sub.stalled {
			continue
		}
		frames, err := sub.journal.ReadAfter(sub.epoch, sub.cursor, maxFramesPerMessage)
		if err != nil {
			sub.stalled = true
			reason := "lagged"
			if errors.Is(err, transcript.ErrEpoch) {
				reason = "epoch"
			}
			data, _ := json.Marshal(resyncMessage{
				Type: "resync_required", SessionID: sub.key, Epoch: sub.journal.Epoch(),
				HeadSeq: sub.journal.Head(), Reason: reason,
			})
			messages = append(messages, data)
			continue
		}
		if len(frames) == 0 {
			continue
		}
		data, sent, trimmed := encodeFrames(sub.key, sub.epoch, frames)
		if data == nil || sent == 0 {
			continue
		}
		messages = append(messages, data)
		sub.cursor = frames[sent-1].Seq
		if trimmed || len(frames) == maxFramesPerMessage {
			more = true
		}
	}
	return messages, more
}

// encodeFrames encodes the longest prefix of frames that fits maxFramesBytes.
// At least one frame is always encoded and trimmed reports that the rest is
// still queued.
func encodeFrames(key, epoch string, frames []transcript.Frame) (data []byte, sent int, trimmed bool) {
	encode := func(count int) []byte {
		encoded, err := json.Marshal(framesMessage{Type: "frames", SessionID: key, Epoch: epoch, Frames: frames[:count]})
		if err != nil {
			return nil
		}
		return encoded
	}

	data = encode(len(frames))
	if data == nil || len(data) <= maxFramesBytes || len(frames) == 1 {
		return data, len(frames), false
	}

	// Binary search the longest prefix that fits; each probe re-encodes exactly
	// the bytes that will be written.
	best := encode(1)
	bestCount := 1
	low, high := 2, len(frames)
	for low <= high {
		mid := (low + high) / 2
		candidate := encode(mid)
		if candidate == nil {
			break
		}
		if len(candidate) <= maxFramesBytes {
			best, bestCount = candidate, mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, bestCount, true
}

// setLatestState records the newest runtime state for this client. Only the
// latest state is ever written; intermediate snapshots are superseded.
func (c *Client) setLatestState(data []byte) {
	c.stateMu.Lock()
	c.latestState = data
	c.stateDirty = true
	c.stateMu.Unlock()
	c.poke()
}

func (c *Client) takeLatestState() []byte {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if !c.stateDirty {
		return nil
	}
	c.stateDirty = false
	return c.latestState
}

// handleSubscribe attaches the client to a session journal. The journal is
// created on first use so a client can subscribe before the session runs.
func (h *Hub) handleSubscribe(c *Client, msg *ClientMessage) {
	if msg.SessionID == "" || h.mux == nil {
		return
	}
	key := transcript.SessionKey(msg.SessionID)
	c.subscribe(key, h.mux.transcripts.Journal(key, h.mux.sessionTimelineDir(key)), msg.Epoch, msg.AfterSeq)
}
