package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHubStateDeliveryCoalescesWithoutDisconnectingSlowClients(t *testing.T) {
	hub := NewHub(&Mux{})
	slow := newClient(hub, nil)
	slow.send = make(chan []byte, 1)
	slow.send <- []byte(`{"type":"occupied"}`)
	healthy := newClient(hub, nil)
	hub.clients[slow] = true
	hub.clients[healthy] = true
	go hub.Run()
	t.Cleanup(func() {
		hub.unregister <- slow
		hub.unregister <- healthy
		hub.Close()
	})

	hub.BroadcastMessage(&WSMessage{Type: "state", RequestID: "state-v1"})
	hub.BroadcastMessage(&WSMessage{Type: "state", RequestID: "state-v2"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		data := readClient(healthy, time.Until(deadline))
		if data == nil {
			t.Fatal("healthy client did not receive the newest state")
		}
		var msg WSMessage
		if json.Unmarshal(data, &msg) == nil && msg.Type == "state" && msg.RequestID == "state-v2" {
			break
		}
	}
	if state := healthy.takeLatestState(); state != nil {
		t.Fatalf("state delivered twice: %s", state)
	}
	var latest WSMessage
	if data := slow.takeLatestState(); data == nil || json.Unmarshal(data, &latest) != nil || latest.RequestID != "state-v2" {
		t.Fatalf("slow client latest state = %s", data)
	}
	if got := hub.ClientCount(); got != 2 {
		t.Fatalf("connected clients = %d, want both clients", got)
	}
}

func TestHubNonStateBackpressureKeepsCompatibleDropBehavior(t *testing.T) {
	hub := NewHub(&Mux{})
	slow := newClient(hub, nil)
	slow.send = make(chan []byte, 1)
	slow.send <- []byte(`{"type":"occupied"}`)
	healthy := newClient(hub, nil)
	healthy.send = make(chan []byte, 4)
	hub.clients[slow] = true
	hub.clients[healthy] = true
	go hub.Run()
	t.Cleanup(func() {
		hub.unregister <- slow
		hub.unregister <- healthy
		hub.Close()
	})

	hub.BroadcastMessage(&WSMessage{Type: "notification", RequestID: "event-v1"})
	select {
	case data := <-healthy.send:
		var msg WSMessage
		if json.Unmarshal(data, &msg) != nil || msg.Type != "notification" || msg.RequestID != "event-v1" {
			t.Fatalf("healthy non-state delivery = %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy client did not receive non-state message")
	}
	time.Sleep(50 * time.Millisecond)
	if got := hub.ClientCount(); got != 2 {
		t.Fatalf("non-state backpressure disconnected a client: count=%d, want 2", got)
	}
}

func TestHubCloseCleansUpAllClientQueues(t *testing.T) {
	hub := NewHub(&Mux{})
	first := newClient(hub, nil)
	second := newClient(hub, nil)
	hub.clients[first] = true
	hub.clients[second] = true
	go hub.Run()
	hub.Close()

	for name, ch := range map[string]<-chan []byte{"first": first.send, "second": second.send} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("%s client queue remained open", name)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s client queue was not closed", name)
		}
	}
	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("clients after Close = %d, want 0", got)
	}
	for name, client := range map[string]*Client{"first": first, "second": second} {
		select {
		case <-client.ctx.Done():
		default:
			t.Fatalf("%s client context remained live after Hub.Close", name)
		}
	}
}
