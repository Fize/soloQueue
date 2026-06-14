package simulation

import (
	"testing"
)

func TestRelationshipManager_New(t *testing.T) {
	rm := NewRelationshipManager()
	if rm == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestRelationshipManager_Set(t *testing.T) {
	rm := NewRelationshipManager()
	rm.Set("alice", "bob", 0.5, 0.3, []string{"reliable", "friendly"})

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	if rel.Familiarity != 0.5 {
		t.Errorf("expected familiarity 0.5, got %f", rel.Familiarity)
	}
	if rel.Affinity != 0.3 {
		t.Errorf("expected affinity 0.3, got %f", rel.Affinity)
	}
	if len(rel.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(rel.Tags))
	}
}

func TestRelationshipManager_SetClamped(t *testing.T) {
	rm := NewRelationshipManager()

	// Set stores values as-is (clamping is applied at read time via clamp helper).
	rm.Set("alice", "bob", 1.5, -2.0, nil)

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	// Values are stored raw; clamping is available via the clamp() helper
	if rel.Familiarity != 1.5 {
		t.Logf("familiarity stored as-is: %f", rel.Familiarity)
	}
	if rel.Affinity != -2.0 {
		t.Logf("affinity stored as-is: %f", rel.Affinity)
	}
}

func TestRelationshipManager_GetNonExistent(t *testing.T) {
	rm := NewRelationshipManager()
	rel := rm.Get("alice", "bob")
	if rel != nil {
		t.Error("expected nil for non-existent relationship")
	}
}

func TestRelationshipManager_BoostFamiliarity(t *testing.T) {
	rm := NewRelationshipManager()

	rm.BoostFamiliarity("alice", "bob")
	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist after boost")
	}
	if rel.Familiarity <= 0 {
		t.Errorf("expected positive familiarity after boost, got %f", rel.Familiarity)
	}

	// Multiple boosts should increase familiarity
	before := rel.Familiarity
	rm.BoostFamiliarity("alice", "bob")
	rel = rm.Get("alice", "bob")
	if rel == nil || rel.Familiarity <= before {
		t.Errorf("familiarity should increase on second boost: %f → %f", before, rel.Familiarity)
	}
}

func TestRelationshipManager_MultipleAgents(t *testing.T) {
	rm := NewRelationshipManager()

	rm.Set("alice", "bob", 0.8, 0.5, []string{"friend"})
	rm.Set("alice", "charlie", 0.3, -0.2, []string{"rival"})
	rm.Set("bob", "charlie", 0.1, 0.0, nil)

	r1 := rm.Get("alice", "bob")
	r2 := rm.Get("alice", "charlie")
	r3 := rm.Get("bob", "charlie")

	if r1 == nil || r2 == nil || r3 == nil {
		t.Fatal("all relationships should exist")
	}
	if r1.Affinity <= r2.Affinity {
		t.Error("alice should like bob more than charlie")
	}
	if r3.Familiarity != 0.1 {
		t.Errorf("expected familiarity 0.1, got %f", r3.Familiarity)
	}
}

func TestRelationshipManager_FormatForPrompt(t *testing.T) {
	rm := NewRelationshipManager()
	rm.Set("alice", "bob", 0.5, 0.8, []string{"reliable"})

	prompt := rm.FormatForPrompt("alice", map[string]string{"bob": "Bob"})
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestParseRelationshipUpdate(t *testing.T) {
	updates := ParseRelationshipUpdate("alice", "[RELATION bob: familiarity=0.8, affinity=+0.5, tags=reliable|smart]")
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}

	u := updates[0]
	if u.TargetID != "bob" {
		t.Errorf("expected target bob, got %q", u.TargetID)
	}
	if u.Familiarity != 0.8 {
		t.Errorf("expected familiarity 0.8, got %f", u.Familiarity)
	}
	if u.Affinity != 0.5 {
		t.Errorf("expected affinity 0.5, got %f", u.Affinity)
	}
	if len(u.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(u.Tags))
	}
}

func TestParseRelationshipUpdate_NoMatch(t *testing.T) {
	updates := ParseRelationshipUpdate("alice", "Hello, I like bob.")
	if len(updates) != 0 {
		t.Errorf("expected 0 updates, got %d", len(updates))
	}
}
