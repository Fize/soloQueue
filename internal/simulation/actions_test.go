package simulation

import (
	"strings"
	"testing"
)

func TestParseActions_MultiLineSay(t *testing.T) {
	// Multi-line SAY: content between [SAY]: and next action line becomes the message.
	content := "[SAY]: Hello everyone,\nThis is a multi-line message.\nI have more to say.\n\n[PASS]"
	actions, _ := ParseActions(content)
	if len(actions) < 2 {
		t.Fatalf("expected at least 2 actions, got %d", len(actions))
	}
	if actions[0].Type != ActionSpeak {
		t.Errorf("expected first action to be speak, got %s", actions[0].Type)
	}
	if !strings.Contains(actions[0].Content, "Hello everyone") {
		t.Errorf("expected speak content to contain greeting, got: %s", actions[0].Content)
	}
	// Find the PASS action
	hasPass := false
	for _, a := range actions {
		if a.Type == ActionPass {
			hasPass = true
		}
	}
	if !hasPass {
		t.Error("expected a PASS action")
	}
}

func TestParseActions_EmptyContent(t *testing.T) {
	actions, proposals := ParseActions("")
	if len(actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(actions))
	}
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestParseActions_AllTypes(t *testing.T) {
	content := "[SAY]: Hello!\n[MOVE cafe]\n[INTERACT menu: read]\n[WAIT 30m]\n[PASS]\n[PROPOSE key: value]"
	actions, proposals := ParseActions(content)

	if len(actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(actions))
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}

	types := []ActionType{ActionSpeak, ActionMove, ActionInteract, ActionWait, ActionPass}
	for i, want := range types {
		if actions[i].Type != want {
			t.Errorf("action[%d]: expected %s, got %s", i, want, actions[i].Type)
		}
	}
}

func TestParseActions_ProposalWithSpaces(t *testing.T) {
	_, proposals := ParseActions("[PROPOSE era: 2025]")
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].key != "era" {
		t.Errorf("expected key 'era', got %q", proposals[0].key)
	}
	if proposals[0].value != "2025" {
		t.Errorf("expected value '2025', got %q", proposals[0].value)
	}
}

func TestParseActions_IgnoreRandomBrackets(t *testing.T) {
	actions, _ := ParseActions("I think [this is interesting] but not an action.")
	if len(actions) != 0 {
		t.Errorf("expected no actions from random brackets, got %d", len(actions))
	}
}

func TestFormatActionsForPrompt(t *testing.T) {
	prompt := FormatActionsForPrompt()
	if !strings.Contains(prompt, "[SAY]:") {
		t.Error("prompt should contain [SAY]:")
	}
	if !strings.Contains(prompt, "[MOVE") {
		t.Error("prompt should contain [MOVE]")
	}
	if !strings.Contains(prompt, "[PASS]") {
		t.Error("prompt should contain [PASS]")
	}
	if !strings.Contains(prompt, "[PROPOSE") {
		t.Error("prompt should contain [PROPOSE]")
	}
}

func TestAction_String(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{Action{Type: ActionSpeak, Target: "*", Content: "hello"}, "[SAY]: hello"},
		{Action{Type: ActionSpeak, Target: "alice", Content: "hi"}, "[SAY @alice]: hi"},
		{Action{Type: ActionMove, Target: "cafe"}, "[MOVE cafe]"},
		{Action{Type: ActionInteract, Target: "menu", Content: "read"}, "[INTERACT menu: read]"},
		{Action{Type: ActionWait, Duration: "30m"}, "[WAIT 30m]"},
		{Action{Type: ActionPass}, "[PASS]"},
		{Action{Type: ActionConflict, Target: "bob", Content: "挑衅他"}, "[CONFLICT @bob]: 挑衅他"},
		{Action{Type: ActionHide}, "[HIDE]"},
	}

	for _, tt := range tests {
		got := tt.action.String()
		if got != tt.want {
			t.Errorf("Action.String() = %q, want %q", got, tt.want)
		}
	}
}
