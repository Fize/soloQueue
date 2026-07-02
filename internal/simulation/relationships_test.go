package simulation

import (
	"strings"
	"testing"
)

// ─── RelationKind helpers ──────────────────────────────────────────────────

func TestRelationKind_IsDirectional(t *testing.T) {
	tests := []struct {
		kind RelationKind
		want bool
	}{
		{RelationParent, true},
		{RelationChild, true},
		{RelationMentor, true},
		{RelationMentee, true},
		{RelationSibling, false},
		{RelationSpouse, false},
		{RelationFriend, false},
		{RelationRival, false},
		{RelationColleague, false},
		{RelationNeighbor, false},
		{RelationStranger, false},
		{"unknown", false},
	}
	for _, tt := range tests {
		got := tt.kind.IsDirectional()
		if got != tt.want {
			t.Errorf("IsDirectional(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestRelationKind_InverseKind(t *testing.T) {
	tests := []struct {
		kind RelationKind
		want RelationKind
	}{
		{RelationParent, RelationChild},
		{RelationChild, RelationParent},
		{RelationMentor, RelationMentee},
		{RelationMentee, RelationMentor},
		{RelationSibling, RelationSibling},
		{RelationSpouse, RelationSpouse},
		{RelationFriend, RelationFriend},
		{RelationRival, RelationRival},
		{RelationColleague, RelationColleague},
		{RelationNeighbor, RelationNeighbor},
		{RelationStranger, RelationStranger},
	}
	for _, tt := range tests {
		got := tt.kind.InverseKind()
		if got != tt.want {
			t.Errorf("InverseKind(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// ─── New: SetWithKind ──────────────────────────────────────────────────────

func TestRelationshipManager_SetWithKind(t *testing.T) {
	rm := NewRelationshipManager()
	rm.SetWithKind("alice", "bob", RelationFriend, 0.8, 0.5, []string{"reliable"})

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	if rel.Kind != RelationFriend {
		t.Errorf("expected kind %q, got %q", RelationFriend, rel.Kind)
	}
	if rel.Familiarity != 0.8 {
		t.Errorf("expected familiarity 0.8, got %f", rel.Familiarity)
	}
	if rel.Affinity != 0.5 {
		t.Errorf("expected affinity 0.5, got %f", rel.Affinity)
	}
	// Kind should be in tags
	found := false
	for _, tag := range rel.Tags {
		if tag == string(RelationFriend) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected kind %q in tags, got %v", RelationFriend, rel.Tags)
	}
}

func TestRelationshipManager_SetWithKind_Update(t *testing.T) {
	rm := NewRelationshipManager()
	rm.SetWithKind("alice", "bob", RelationFriend, 0.8, 0.5, []string{"reliable"})
	rm.SetWithKind("alice", "bob", RelationRival, 0.3, -0.4, []string{"competitive"})

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	// Kind should be updated
	if rel.Kind != RelationRival {
		t.Errorf("expected kind %q, got %q", RelationRival, rel.Kind)
	}
	// Familiarity should be averaged: (0.8 + 0.3) / 2 = 0.55
	if rel.Familiarity != 0.55 {
		t.Errorf("expected averaged familiarity 0.55, got %f", rel.Familiarity)
	}
	// Tags should be merged (reliable + competitive + kind)
	if len(rel.Tags) != 4 {
		t.Errorf("expected 4 merged tags (friend+reliable+competitive+rival), got %v", rel.Tags)
	}
}

// ─── New: SetKind / KindOf ─────────────────────────────────────────────────

func TestRelationshipManager_SetKind(t *testing.T) {
	rm := NewRelationshipManager()

	// SetKind on non-existent creates with defaults
	rm.SetKind("alice", "bob", RelationFriend)
	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	if rel.Kind != RelationFriend {
		t.Errorf("expected kind %q, got %q", RelationFriend, rel.Kind)
	}
	if rel.Familiarity != 0.5 {
		t.Errorf("expected default familiarity 0.5, got %f", rel.Familiarity)
	}

	// SetKind on existing updates only the kind
	rm.SetKind("alice", "bob", RelationRival)
	rel = rm.Get("alice", "bob")
	if rel.Kind != RelationRival {
		t.Errorf("expected kind %q, got %q", RelationRival, rel.Kind)
	}
	if rel.Familiarity != 0.5 {
		t.Errorf("familiarity should be preserved at 0.5, got %f", rel.Familiarity)
	}
}

func TestRelationshipManager_KindOf(t *testing.T) {
	rm := NewRelationshipManager()

	// No relationship → stranger
	if kind := rm.KindOf("alice", "bob"); kind != RelationStranger {
		t.Errorf("expected stranger, got %q", kind)
	}

	rm.SetWithKind("alice", "bob", RelationSibling, 0.9, 0.7, nil)
	if kind := rm.KindOf("alice", "bob"); kind != RelationSibling {
		t.Errorf("expected sibling, got %q", kind)
	}
}

// ─── New: BulkInit ──────────────────────────────────────────────────────────

func TestRelationshipManager_BulkInit(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{
		"moderator": "Mayor Chen",
		"supporter": "Engineer Li",
		"skeptic":   "Doctor Wang",
		"economist": "Shopkeeper Zhang",
	}

	rels := []InitialRelationship{
		{SubjectName: "Mayor Chen", TargetName: "Shopkeeper Zhang", Kind: RelationFriend, Familiarity: 0.9, Affinity: 0.8},
		{SubjectName: "Mayor Chen", TargetName: "Engineer Li", Kind: RelationFriend, Familiarity: 0.7, Affinity: 0.6},
		{SubjectName: "Mayor Chen", TargetName: "Doctor Wang", Kind: RelationNeighbor, Familiarity: 0.2, Affinity: 0.1},
	}

	err := rm.BulkInit(rels, nameByID)
	if err != nil {
		t.Fatalf("BulkInit error: %v", err)
	}

	// Friend is bidirectional, so both directions should be set
	r1 := rm.Get("moderator", "economist")
	if r1 == nil || r1.Kind != RelationFriend {
		t.Errorf("Mayor Chen→Shopkeeper Zhang: expected kind %q, got %v", RelationFriend, r1)
	}
	if r1.Familiarity != 0.9 {
		t.Errorf("Mayor Chen→Shopkeeper Zhang: expected familiarity 0.9, got %f", r1.Familiarity)
	}
	r1rev := rm.Get("economist", "moderator")
	if r1rev == nil || r1rev.Kind != RelationFriend {
		t.Errorf("Shopkeeper Zhang→Mayor Chen (bidirectional): expected kind %q, got %v", RelationFriend, r1rev)
	}

	// Neighbor is also bidirectional
	r3 := rm.Get("moderator", "skeptic")
	if r3 == nil || r3.Kind != RelationNeighbor {
		t.Errorf("Mayor Chen→Doctor Wang: expected kind %q, got %v", RelationNeighbor, r3)
	}
	if r3.Familiarity != 0.2 {
		t.Errorf("Mayor Chen→Doctor Wang: expected familiarity 0.2, got %f", r3.Familiarity)
	}
	r3rev := rm.Get("skeptic", "moderator")
	if r3rev == nil || r3rev.Kind != RelationNeighbor {
		t.Errorf("Doctor Wang→Mayor Chen (bidirectional): expected kind %q, got %v", RelationNeighbor, r3rev)
	}
}

func TestRelationshipManager_BulkInit_Directional(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{
		"parent": "Father Chen",
		"child":  "Xiao Ming Chen",
	}

	rels := []InitialRelationship{
		{SubjectName: "Father Chen", TargetName: "Xiao Ming Chen", Kind: RelationParent, Familiarity: 1.0, Affinity: 0.9},
	}

	err := rm.BulkInit(rels, nameByID)
	if err != nil {
		t.Fatalf("BulkInit error: %v", err)
	}

	// Parent→Child: should have kind parent
	r := rm.Get("parent", "child")
	if r == nil || r.Kind != RelationParent {
		t.Errorf("parent→child: expected kind %q, got %v", RelationParent, r)
	}

	// Child→Parent: should have kind child (inverse)
	rRev := rm.Get("child", "parent")
	if rRev == nil || rRev.Kind != RelationChild {
		t.Errorf("child→parent: expected kind %q, got %v", RelationChild, rRev)
	}
	// Inverse familiarity should be slightly less
	if rRev.Familiarity >= 1.0 {
		t.Errorf("inverse familiarity should be discounted (< 1.0), got %f", rRev.Familiarity)
	}
}

func TestRelationshipManager_BulkInit_NameCaseInsensitive(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{
		"alice": "Alice Smith",
		"bob":   "Bob Jones",
	}

	// Use lowercase name that doesn't exactly match
	rels := []InitialRelationship{
		{SubjectName: "alice smith", TargetName: "bob jones", Kind: RelationFriend, Familiarity: 0.8, Affinity: 0.5},
	}

	err := rm.BulkInit(rels, nameByID)
	if err != nil {
		t.Fatalf("BulkInit error: %v", err)
	}
	if rm.Get("alice", "bob") == nil {
		t.Error("expected relationship to be resolved case-insensitively")
	}
}

func TestRelationshipManager_BulkInit_SkipSelf(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{"alice": "Alice"}

	rels := []InitialRelationship{
		{SubjectName: "Alice", TargetName: "Alice", Kind: RelationFriend, Familiarity: 1.0, Affinity: 1.0},
	}

	err := rm.BulkInit(rels, nameByID)
	if err != nil {
		t.Fatalf("BulkInit error: %v", err)
	}
	if rm.Get("alice", "alice") != nil {
		t.Error("self-relationship should be skipped")
	}
}

func TestRelationshipManager_BulkInit_UnknownNames(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{"alice": "Alice"}

	rels := []InitialRelationship{
		{SubjectName: "Alice", TargetName: "NonExistent", Kind: RelationFriend, Familiarity: 0.5, Affinity: 0},
	}

	err := rm.BulkInit(rels, nameByID)
	if err != nil {
		t.Fatalf("BulkInit error: %v", err)
	}
	// Should not panic, should silently skip
	if rm.Get("alice", "nonexistent") != nil {
		t.Error("relationship with unknown target should not be created")
	}
}

// ─── New: AllRelationships ─────────────────────────────────────────────────

func TestRelationshipManager_AllRelationships(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{
		"alice": "Alice",
		"bob":   "Bob",
	}

	rm.SetWithKind("alice", "bob", RelationFriend, 0.8, 0.5, []string{"reliable"})
	rm.SetWithKind("bob", "alice", RelationFriend, 0.7, 0.4, nil)

	dtos := rm.AllRelationships(nameByID)
	if len(dtos) != 2 {
		t.Fatalf("expected 2 DTOs, got %d", len(dtos))
	}

	// Check that names are populated
	for _, dto := range dtos {
		if dto.SubjectName == "" || dto.TargetName == "" {
			t.Errorf("expected populated names, got subject=%q target=%q", dto.SubjectName, dto.TargetName)
		}
		if dto.Kind == "" {
			t.Error("expected non-empty kind (default stranger)")
		}
	}
}

func TestRelationshipManager_AllRelationships_Empty(t *testing.T) {
	rm := NewRelationshipManager()
	dtos := rm.AllRelationships(map[string]string{})
	if len(dtos) != 0 {
		t.Errorf("expected empty DTOs, got %d", len(dtos))
	}
}

// ─── New: ParseRelationshipUpdate with kind= ─────────────────────────────────

func TestParseRelationshipUpdate_WithKind(t *testing.T) {
	updates := ParseRelationshipUpdate("alice",
		"[RELATION bob: kind=friend, familiarity=0.8, affinity=+0.5, tags=reliable|smart]")

	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}

	u := updates[0]
	if u.Kind != RelationFriend {
		t.Errorf("expected kind %q, got %q", RelationFriend, u.Kind)
	}
	if u.TargetID != "bob" {
		t.Errorf("expected target 'bob', got %q", u.TargetID)
	}
	if u.Familiarity != 0.8 {
		t.Errorf("expected familiarity 0.8, got %f", u.Familiarity)
	}
	if u.Affinity != 0.5 {
		t.Errorf("expected affinity 0.5, got %f", u.Affinity)
	}
	if len(u.Tags) != 2 {
		t.Errorf("expected 2 tags, got %v", u.Tags)
	}
}

func TestParseRelationshipUpdate_KindOnly(t *testing.T) {
	updates := ParseRelationshipUpdate("alice", "[RELATION bob: kind=rival]")
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].Kind != RelationRival {
		t.Errorf("expected kind %q, got %q", RelationRival, updates[0].Kind)
	}
	if updates[0].TargetID != "bob" {
		t.Errorf("expected target 'bob', got %q", updates[0].TargetID)
	}
	// Familiarity and affinity should be zero when not specified
	if updates[0].Familiarity != 0 {
		t.Errorf("expected familiarity 0, got %f", updates[0].Familiarity)
	}
}

func TestParseRelationshipUpdate_Multiple(t *testing.T) {
	content := "[RELATION bob: kind=friend, familiarity=0.8]\n[RELATION charlie: kind=rival, affinity=-0.5]"
	updates := ParseRelationshipUpdate("alice", content)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[0].Kind != RelationFriend || updates[0].TargetID != "bob" {
		t.Errorf("first update: expected bob/friend, got %v", updates[0])
	}
	if updates[1].Kind != RelationRival || updates[1].TargetID != "charlie" {
		t.Errorf("second update: expected charlie/rival, got %v", updates[1])
	}
}

// ─── New: FormatForPrompt with kinds ───────────────────────────────────────

func TestFormatForPrompt_WithKinds(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{
		"alice": "Alice",
		"bob":   "Bob",
		"charlie": "Charlie",
	}

	rm.SetWithKind("alice", "bob", RelationFriend, 0.9, 0.8, []string{"close"})
	rm.SetWithKind("alice", "charlie", RelationRival, 0.3, -0.5, []string{"competitive"})

	prompt := rm.FormatForPrompt("alice", nameByID, "en")
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}

	// Should have a friends section
	if !strings.Contains(prompt, "Friends") {
		t.Error("prompt should contain 'Friends' section")
	}
	if !strings.Contains(prompt, "Bob") {
		t.Error("prompt should contain Bob's name")
	}
	if !strings.Contains(prompt, "Rivals") {
		t.Error("prompt should contain 'Rivals' section")
	}
	if !strings.Contains(prompt, "Charlie") {
		t.Error("prompt should contain Charlie's name")
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	rm := NewRelationshipManager()
	prompt := rm.FormatForPrompt("alice", map[string]string{"alice": "Alice"}, "en")
	if !strings.Contains(prompt, "haven't formed") {
		t.Error("expected empty relationship message")
	}
}

func TestFormatForPrompt_FamilySection(t *testing.T) {
	rm := NewRelationshipManager()
	nameByID := map[string]string{"alice": "Alice", "bob": "Bob"}

	rm.SetWithKind("alice", "bob", RelationSibling, 0.95, 0.9, nil)

	prompt := rm.FormatForPrompt("alice", nameByID, "en")
	if !strings.Contains(prompt, "Family") {
		t.Error("prompt should contain 'Family' section for sibling")
	}
	if !strings.Contains(prompt, "close") {
		t.Error("prompt should describe close relationship")
	}
}

// ─── Existing: backfill for backward compat ─────────────────────────────────

func TestRelationshipManager_Set_BackwardCompat(t *testing.T) {
	rm := NewRelationshipManager()
	rm.Set("alice", "bob", 0.5, 0.3, []string{"reliable"})

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
	// Set should leave Kind empty (treated as stranger)
	if rel.Kind != "" {
		t.Errorf("Set() should not set Kind, got %q", rel.Kind)
	}
	if rel.Familiarity != 0.5 {
		t.Errorf("expected familiarity 0.5, got %f", rel.Familiarity)
	}
}

func TestRelationshipManager_New(t *testing.T) {
	rm := NewRelationshipManager()
	if rm == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestRelationshipManager_SetClamped(t *testing.T) {
	rm := NewRelationshipManager()

	rm.Set("alice", "bob", 1.5, -2.0, nil)

	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship to exist")
	}
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

	prompt := rm.FormatForPrompt("alice", map[string]string{"bob": "Bob"}, "en")
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

func TestRelationshipManager_UpdateAffinity(t *testing.T) {
	rm := NewRelationshipManager()

	rm.UpdateAffinity("alice", "bob", 0.5)
	rel := rm.Get("alice", "bob")
	if rel == nil {
		t.Fatal("expected relationship after update")
	}
	if rel.Affinity != 0.5 {
		t.Errorf("expected affinity 0.5, got %f", rel.Affinity)
	}

	rm.UpdateAffinity("alice", "bob", -0.8)
	rel = rm.Get("alice", "bob")
	if rel.Affinity < -0.31 || rel.Affinity > -0.29 {
		t.Errorf("expected affinity around -0.3, got %f", rel.Affinity)
	}
}