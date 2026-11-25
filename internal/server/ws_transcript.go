package server

import (
	"encoding/json"
	"errors"

	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// maxFramesPerMessage bounds one batched WebSocket message.
const maxFramesPerMessage = 512

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
		data, err := json.Marshal(framesMessage{Type: "frames", SessionID: sub.key, Epoch: sub.epoch, Frames: frames})
		if err != nil {
			continue
		}
		messages = append(messages, data)
		sub.cursor = frames[len(frames)-1].Seq
		if len(frames) == maxFramesPerMessage {
			more = true
		}
	}
	return messages, more
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
