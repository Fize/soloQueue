package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

func decodeServerMessage(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return msg
}

func messageType(msg map[string]json.RawMessage) string {
	var typ string
	_ = json.Unmarshal(msg["type"], &typ)
	return typ
}

func TestSubscriptionBatchesFramesAndRequestsResync(t *testing.T) {
	hub := NewHub(&Mux{})
	client := newClient(hub, nil)
	reg := transcript.NewRegistry(transcript.Config{MaxFrames: 4})
	j := reg.Journal("l2:batch", "")
	j.Publish(transcript.Frame{Kind: transcript.KindSessionMeta})
	client.subscribe("l2:batch", j, j.Epoch(), 0)
	for i := 0; i < 2; i++ {
		j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: fmt.Sprint(i)})
	}
	messages, more := client.pendingTranscriptMessages()
	if more || len(messages) != 1 {
		t.Fatalf("messages=%d more=%v", len(messages), more)
	}
	var batch framesMessage
	if err := json.Unmarshal(messages[0], &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Type != "frames" || batch.SessionID != "l2:batch" || len(batch.Frames) != 3 || batch.Frames[0].Seq != 1 || batch.Frames[2].Seq != 3 {
		t.Fatalf("batch = %#v", batch)
	}
	if messages, _ := client.pendingTranscriptMessages(); len(messages) != 0 {
		t.Fatalf("frames delivered twice: %d messages", len(messages))
	}

	// A cursor that fell out of the ring is never served partially.
	client.subscribe("l2:batch", j, j.Epoch(), 0)
	for i := 0; i < 5; i++ {
		j.Publish(transcript.Frame{Kind: transcript.KindSessionMeta})
	}
	messages, _ = client.pendingTranscriptMessages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	var resync resyncMessage
	if err := json.Unmarshal(messages[0], &resync); err != nil {
		t.Fatal(err)
	}
	if resync.Type != "resync_required" || resync.Reason != "lagged" || resync.HeadSeq != j.Head() || resync.Epoch != j.Epoch() {
		t.Fatalf("resync = %#v", resync)
	}
	if messages, _ := client.pendingTranscriptMessages(); len(messages) != 0 {
		t.Fatal("a stalled subscription kept producing messages")
	}

	client.subscribe("l2:batch", j, "stale-epoch", 3)
	messages, _ = client.pendingTranscriptMessages()
	if err := json.Unmarshal(messages[0], &resync); err != nil || resync.Reason != "epoch" {
		t.Fatalf("epoch resync = %#v %v", resync, err)
	}
	client.closeSubscriptions()
}

func TestWebSocketTranscriptSubscriptionResumes(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()
	hub := NewHub(mux)
	mux.SetHub(hub)
	go hub.Run()
	defer hub.Close()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	key := fmt.Sprintf("l2:ws%d", time.Now().UnixNano())
	j := mux.transcripts.Journal(key, mux.sessionTimelineDir(key))

	dial := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		return conn
	}
	// collect reads frames of key until want sequences were seen.
	collect := func(conn *websocket.Conn, want int) []transcript.Frame {
		t.Helper()
		var frames []transcript.Frame
		for len(frames) < want {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read: %v (have %d frames)", err, len(frames))
			}
			msg := decodeServerMessage(t, data)
			if messageType(msg) != "frames" {
				continue
			}
			var batch framesMessage
			if err := json.Unmarshal(data, &batch); err != nil || batch.SessionID != key {
				continue
			}
			frames = append(frames, batch.Frames...)
		}
		return frames
	}

	conn := dial()
	if err := conn.WriteJSON(ClientMessage{Type: "subscribe", SessionID: key}); err != nil {
		t.Fatal(err)
	}
	// The subscription starts at the head; wait until it is registered.
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.RLock()
		registered := false
		for c := range hub.clients {
			c.subsMu.Lock()
			_, registered = c.subs[key]
			c.subsMu.Unlock()
		}
		hub.mu.RUnlock()
		if registered || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, text := range []string{"a", "b", "c"} {
		j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: text})
	}
	first := collect(conn, 3)
	if first[0].Text != "a" || first[2].Text != "c" || first[2].Seq != first[0].Seq+2 {
		t.Fatalf("live frames = %#v", first)
	}
	_ = conn.Close()

	// Output produced while disconnected is replayed after the cursor.
	j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: "d"})
	j.Publish(transcript.Frame{Kind: transcript.KindRequestEnd, RequestID: "r"})
	conn = dial()
	defer conn.Close()
	if err := conn.WriteJSON(ClientMessage{Type: "subscribe", SessionID: key, Epoch: j.Epoch(), AfterSeq: first[2].Seq}); err != nil {
		t.Fatal(err)
	}
	missed := collect(conn, 2)
	if missed[0].Text != "d" || missed[0].Seq != first[2].Seq+1 || missed[1].Kind != transcript.KindRequestEnd {
		t.Fatalf("replayed frames = %#v", missed)
	}

	if err := conn.WriteJSON(ClientMessage{Type: "subscribe", SessionID: key, Epoch: "previous-process", AfterSeq: 1}); err != nil {
		t.Fatal(err)
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		msg := decodeServerMessage(t, data)
		if messageType(msg) != "resync_required" {
			continue
		}
		var resync resyncMessage
		_ = json.Unmarshal(data, &resync)
		if resync.SessionID != key || resync.Reason != "epoch" || resync.Epoch != j.Epoch() {
			t.Fatalf("resync = %#v", resync)
		}
		return
	}
}

func TestFramesMessageIsBoundedByBytes(t *testing.T) {
	hub := NewHub(&Mux{})
	client := newClient(hub, nil)
	reg := transcript.NewRegistry(transcript.Config{MaxFrames: 2048})
	j := reg.Journal("l2:bytes", "")
	// Each frame alone is small, but the batch is far above the byte budget.
	payload := strings.Repeat("x", 8<<10)
	const total = 200
	for i := 0; i < total; i++ {
		j.Publish(transcript.Frame{Kind: transcript.KindItemDelta, RequestID: "r", ItemID: "out:r:0", Text: payload})
	}
	client.subscribe("l2:bytes", j, j.Epoch(), 0)

	seen := 0
	for round := 0; round < 100; round++ {
		messages, more := client.pendingTranscriptMessages()
		for _, raw := range messages {
			if len(raw) > maxFramesBytes+(16<<10) {
				t.Fatalf("message %d bytes exceeds the budget", len(raw))
			}
			var batch framesMessage
			if err := json.Unmarshal(raw, &batch); err != nil {
				t.Fatal(err)
			}
			if len(batch.Frames) == 0 {
				t.Fatal("empty batch")
			}
			for _, frame := range batch.Frames {
				if frame.Seq != uint64(seen+1) {
					t.Fatalf("sequence gap: got %d want %d", frame.Seq, seen+1)
				}
				seen++
			}
		}
		if !more && len(messages) == 0 {
			break
		}
	}
	if seen != total {
		t.Fatalf("delivered %d of %d frames", seen, total)
	}
}

func TestOversizedSingleFrameIsStillDelivered(t *testing.T) {
	hub := NewHub(&Mux{})
	client := newClient(hub, nil)
	reg := transcript.NewRegistry(transcript.Config{MaxFrames: 16})
	j := reg.Journal("l2:big", "")
	j.Publish(transcript.Frame{Kind: transcript.KindItemSnapshot, RequestID: "r", ItemID: "out:r:0",
		Segments: []transcript.Segment{{Type: transcript.SegmentContent, Text: strings.Repeat("y", maxFramesBytes*2)}}})
	client.subscribe("l2:big", j, j.Epoch(), 0)

	messages, more := client.pendingTranscriptMessages()
	if len(messages) != 1 || more {
		t.Fatalf("messages=%d more=%v", len(messages), more)
	}
	var batch framesMessage
	if err := json.Unmarshal(messages[0], &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Frames) != 1 || batch.Frames[0].Seq != 1 {
		t.Fatalf("batch = %#v", batch)
	}
	if messages, _ := client.pendingTranscriptMessages(); len(messages) != 0 {
		t.Fatalf("frame delivered twice: %d", len(messages))
	}
}
