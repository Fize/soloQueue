package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// Runtime state only drives status indicators, and every request lifecycle step
// asks for a broadcast. The Hub must coalesce those requests: a burst of changes
// may not become a burst of messages. The design bound is four per second.
func TestHubBroadcastsRuntimeStateAtMostFourTimesPerSecond(t *testing.T) {
	hub := NewHub(&Mux{})
	client := newClient(hub, nil)
	go hub.Run()
	t.Cleanup(func() {
		hub.unregister <- client
		hub.Close()
	})
	hub.register <- client
	if readClient(client, 2*time.Second) == nil {
		t.Fatal("initial state was not delivered")
	}

	// A notification every 5 ms is about 320 requests for a broadcast.
	const window = 1600 * time.Millisecond
	stop := make(chan struct{})
	var notifier sync.WaitGroup
	notifier.Add(1)
	go func() {
		defer notifier.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				hub.Notify()
			}
		}
	}()

	states := 0
	deadline := time.Now().Add(window)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		data := readClient(client, remaining)
		if data == nil {
			break
		}
		var msg WSMessage
		if json.Unmarshal(data, &msg) == nil && msg.Type == "state" {
			states++
		}
	}
	close(stop)
	notifier.Wait()

	// Four per second is 6.4 in the window, plus one at its leading edge.
	if states > 8 {
		t.Fatalf("%d state broadcasts in %v, want at most 4 per second", states, window)
	}
	if states < 3 {
		t.Fatalf("only %d state broadcasts in %v: state stopped flowing", states, window)
	}
}
