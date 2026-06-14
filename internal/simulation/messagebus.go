package simulation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Message represents a message sent between simulation agents.
type Message struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
	Round   int    `json:"round"`
	Type    string `json:"type"`
}

// MessageBus enables peer-to-peer and broadcast messaging between agents.
// Supports event-driven notification: each agent gets a notifyCh that wakes
// it up when new messages arrive, avoiding polling.
type MessageBus struct {
	subscribers map[string]chan Message
	notifyChs   map[string]chan struct{}
	mu          sync.RWMutex
	buffer      int
}

func NewMessageBus(buffer int) *MessageBus {
	if buffer <= 0 {
		buffer = 64
	}
	return &MessageBus{
		subscribers: make(map[string]chan Message),
		notifyChs:   make(map[string]chan struct{}),
		buffer:      buffer,
	}
}

func (mb *MessageBus) Register(personaID string) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if _, exists := mb.subscribers[personaID]; !exists {
		mb.subscribers[personaID] = make(chan Message, mb.buffer)
		mb.notifyChs[personaID] = make(chan struct{}, mb.buffer)
	}
}

func (mb *MessageBus) Unregister(personaID string) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if ch, ok := mb.subscribers[personaID]; ok {
		close(ch)
		delete(mb.subscribers, personaID)
	}
	if nch, ok := mb.notifyChs[personaID]; ok {
		close(nch)
		delete(mb.notifyChs, personaID)
	}
}

// NotifyCh returns a channel that receives when new messages are available.
// The agent goroutine blocks on this instead of polling.
func (mb *MessageBus) NotifyCh(personaID string) <-chan struct{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	if ch, ok := mb.notifyChs[personaID]; ok {
		return ch
	}
	return nil
}

// Send delivers a message and wakes the target agent.
func (mb *MessageBus) Send(to string, msg Message) {
	mb.mu.RLock()
	ch, ok := mb.subscribers[to]
	nch := mb.notifyChs[to]
	mb.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
		mb.notify(to, nch)
	default:
	}
}

// Broadcast sends a message to all subscribers except the sender, waking each.
func (mb *MessageBus) Broadcast(from string, msg Message) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	for id, ch := range mb.subscribers {
		if id == from {
			continue
		}
		select {
		case ch <- msg:
			if nch, ok := mb.notifyChs[id]; ok {
				mb.notify(id, nch)
			}
		default:
		}
	}
}

func (mb *MessageBus) notify(id string, nch chan struct{}) {
	select {
	case nch <- struct{}{}:
	default:
	}
}

// DrainAll collects all pending messages and drains the notification channel.
func (mb *MessageBus) DrainAll(personaID string) []Message {
	mb.mu.RLock()
	ch, ok := mb.subscribers[personaID]
	nch := mb.notifyChs[personaID]
	mb.mu.RUnlock()
	if !ok {
		return nil
	}

	var msgs []Message
	for {
		select {
		case msg := <-ch:
			msgs = append(msgs, msg)
		default:
			// Drain any accumulated notifications
			for {
				select {
				case <-nch:
				default:
					return msgs
				}
			}
		}
	}
}

// SendPrivate delivers a message only to a single target agent. Unlike Send and
// Broadcast which are used for public messages, this is used for private dialogue.
func (mb *MessageBus) SendPrivate(from, to string, msg Message) {
	mb.mu.RLock()
	ch, ok := mb.subscribers[to]
	nch := mb.notifyChs[to]
	mb.mu.RUnlock()
	if !ok {
		return
	}
	msg.From = from
	msg.To = to
	msg.Type = "private"
	select {
	case ch <- msg:
		mb.notify(to, nch)
	default:
	}
}

// FormatMessages renders a slice of messages as structured text for prompt injection.
func FormatMessages(msgs []Message) string {
	if len(msgs) == 0 {
		return "(no messages)"
	}

	sort.Slice(msgs, func(i, j int) bool {
		if msgs[i].Round != msgs[j].Round {
			return msgs[i].Round < msgs[j].Round
		}
		return msgs[i].From < msgs[j].From
	})

	var b strings.Builder
	b.WriteString("## Messages from other participants\n")
	for _, m := range msgs {
		b.WriteString(fmt.Sprintf("[%s] (%s): %s\n", m.From, m.Type, m.Content))
	}
	return b.String()
}
