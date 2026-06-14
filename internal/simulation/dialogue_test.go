package simulation

import (
	"testing"
)

func TestDialogueSession_New(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 10)
	if !ds.Active() {
		t.Error("new session should be active")
	}
	if ds.InitiatorID != "alice" {
		t.Errorf("expected initiator alice, got %s", ds.InitiatorID)
	}
	if ds.TargetID != "bob" {
		t.Errorf("expected target bob, got %s", ds.TargetID)
	}
	if ds.Turns() != 0 {
		t.Errorf("expected 0 turns, got %d", ds.Turns())
	}
}

func TestDialogueSession_ZeroMaxTurns(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 0)
	if ds.MaxTurns != 10 {
		t.Errorf("expected max turns 10 (default), got %d", ds.MaxTurns)
	}
}

func TestDialogueSession_AddTurn(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 2)

	ds.AddTurn(Message{From: "alice", Content: "hello"})
	if ds.Turns() != 1 {
		t.Errorf("expected 1 turn, got %d", ds.Turns())
	}
	if !ds.Active() {
		t.Error("should still be active after 1 turn")
	}

	ds.AddTurn(Message{From: "bob", Content: "hi"})
	if ds.Turns() != 2 {
		t.Errorf("expected 2 turns, got %d", ds.Turns())
	}
	if ds.Active() {
		t.Error("should be inactive after reaching max turns")
	}
}

func TestDialogueSession_End(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 10)
	ds.End()
	if ds.Active() {
		t.Error("should be inactive after End()")
	}
}

func TestDialogueSession_History(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 10)
	ds.AddTurn(Message{From: "alice", Content: "hello"})
	ds.AddTurn(Message{From: "bob", Content: "hi"})

	history := ds.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", history[0].Content)
	}
}

func TestDialogueSession_InSession(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 10)
	if !ds.InSession("alice") {
		t.Error("alice should be in session")
	}
	if !ds.InSession("bob") {
		t.Error("bob should be in session")
	}
	if ds.InSession("charlie") {
		t.Error("charlie should not be in session")
	}
}

func TestDialogueSession_Other(t *testing.T) {
	ds := NewDialogueSession("alice", "bob", 10)
	if ds.Other("alice") != "bob" {
		t.Errorf("alice's other should be bob, got %s", ds.Other("alice"))
	}
	if ds.Other("bob") != "alice" {
		t.Errorf("bob's other should be alice, got %s", ds.Other("bob"))
	}
}

func TestDialogueManager_Request(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	err := dm.Request("alice", "bob")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if !dm.IsAgentInDialogue("alice") {
		t.Error("alice should be in dialogue")
	}
	if !dm.IsAgentInDialogue("bob") {
		t.Error("bob should be in dialogue")
	}

	// Bob should have received a dialogue request
	msgs := bus.DrainAll("bob")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for bob, got %d", len(msgs))
	}
	if msgs[0].Type != "dialogue_request" {
		t.Errorf("expected dialogue_request, got %s", msgs[0].Type)
	}
}

func TestDialogueManager_DuplicateRequest(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")
	err := dm.Request("alice", "bob")
	if err == nil {
		t.Error("expected error for duplicate request")
	}
}

func TestDialogueManager_AlreadyInDialogue(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("charlie")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")
	err := dm.Request("alice", "charlie")
	if err == nil {
		t.Error("expected error for alice already in dialogue")
	}
}

func TestDialogueManager_SendMessage(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")
	err := dm.SendMessage("alice", "hello bob")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	msgs := bus.DrainAll("bob")
	// bob should have: the initial request + the response
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for bob, got %d", len(msgs))
	}
}

func TestDialogueManager_SendMessageNotInDialogue(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")

	dm := NewDialogueManager(bus)

	err := dm.SendMessage("alice", "hello")
	if err == nil {
		t.Error("expected error for agent not in dialogue")
	}
}

func TestDialogueManager_EndDialogue(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")
	dm.EndDialogue("alice")

	if dm.IsAgentInDialogue("alice") {
		t.Error("alice should not be in dialogue after EndDialogue")
	}
	if dm.IsAgentInDialogue("bob") {
		t.Error("bob should not be in dialogue after EndDialogue")
	}
}

func TestDialogueManager_GetDialoguePartner(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")

	if dm.GetDialoguePartner("alice") != "bob" {
		t.Error("alice's partner should be bob")
	}
	if dm.GetDialoguePartner("charlie") != "" {
		t.Error("charlie should have no partner")
	}
}

func TestDialogueManager_GetSession(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	dm := NewDialogueManager(bus)

	dm.Request("alice", "bob")

	sess := dm.GetSession("alice")
	if sess == nil {
		t.Fatal("expected session for alice")
	}
	if sess.InitiatorID != "alice" {
		t.Errorf("expected initiator alice, got %s", sess.InitiatorID)
	}
}

func TestSessionKey(t *testing.T) {
	// Session key should be deterministic and order-independent
	k1 := sessionKey("alice", "bob")
	k2 := sessionKey("bob", "alice")
	if k1 != k2 {
		t.Errorf("session keys should be equal: %q vs %q", k1, k2)
	}
}

func TestSessKeyOther(t *testing.T) {
	if sessKeyOther("alice:bob", "alice") != "bob" {
		t.Error("expected bob")
	}
	if sessKeyOther("alice:bob", "bob") != "alice" {
		t.Error("expected alice")
	}
	if sessKeyOther("invalid", "alice") != "" {
		t.Error("expected empty for invalid key")
	}
}
