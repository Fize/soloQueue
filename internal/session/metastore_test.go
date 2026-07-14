package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ─── metastore tests ────────────────────────────────────────────────────────

// writeRawFile is a tiny helper that writes a file with the given bytes.
func writeRawFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadMeta_PreferMetaJSON(t *testing.T) {
	dir := t.TempDir()
	id := "abc"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	metaPath := filepath.Join(tlDir, MetaFileName)

	// meta.json with all fields populated.
	want := &SessionMeta{
		SchemaVersion: CurrentSchemaVersion,
		Name:          "from-json",
		Group:         "dev",
		WorkDir:       "/work",
		GitBaseRef:    "deadbeef",
		Level:         "L2",
		Plans:         []string{"plan/x.md"},
		Baseline:      map[string]string{"file/a.go": "hash-a"},
	}
	data, _ := json.Marshal(want)
	writeRawFile(t, metaPath, data)

	// Legacy files exist with conflicting data; LoadMeta must ignore them.
	writeRawFile(t, filepath.Join(tlDir, "meta"), []byte(`{"name":"LEGACY","group":"legacy-grp"}`))
	writeRawFile(t, filepath.Join(tlDir, "level"), []byte("LEGACY-LEVEL"))
	writeRawFile(t, filepath.Join(tlDir, "baseline"), []byte("legacy-hash\tlegacy/path\n"))
	writeRawFile(t, filepath.Join(tlDir, "group"), []byte("legacy-group-text"))

	got, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.Name != "from-json" {
		t.Errorf("Name = %q, want from-json", got.Name)
	}
	if got.Group != "dev" {
		t.Errorf("Group = %q, want dev", got.Group)
	}
	if got.GitBaseRef != "deadbeef" {
		t.Errorf("GitBaseRef = %q, want deadbeef", got.GitBaseRef)
	}
	if got.Level != "L2" {
		t.Errorf("Level = %q, want L2", got.Level)
	}
	if len(got.Plans) != 1 || got.Plans[0] != "plan/x.md" {
		t.Errorf("Plans = %v, want [plan/x.md]", got.Plans)
	}
	if got.Baseline["file/a.go"] != "hash-a" {
		t.Errorf("Baseline = %v, want hash-a for file/a.go", got.Baseline)
	}

	// Legacy files must still be on disk because LoadMeta only removes them
	// when meta.json was missing.
	if _, err := os.Stat(filepath.Join(tlDir, "group")); err != nil {
		t.Errorf("legacy group should still exist when meta.json is present, err=%v", err)
	}
}

func TestLoadMeta_GenerateMinimalFromGroupFile(t *testing.T) {
	dir := t.TempDir()
	id := "skeleton"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)

	// No meta.json. Only the legacy `group` file is present — this is the
	// "pre-migration" shape. LoadMeta must extract the group, build a
	// minimal skeleton, persist it to meta.json, and return it.
	writeRawFile(t, filepath.Join(tlDir, "group"), []byte("legacy-only-grp\n"))

	got, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, CurrentSchemaVersion)
	}
	if got.Group != "legacy-only-grp" {
		t.Errorf("Group = %q, want legacy-only-grp", got.Group)
	}
	// Every other field is the zero value.
	if got.Name != "" || got.WorkDir != "" || got.GitBaseRef != "" || got.Level != "" {
		t.Errorf("skeleton should be minimal, got %+v", got)
	}
	if len(got.Plans) != 0 || got.Baseline != nil {
		t.Errorf("skeleton should have no plans/baseline, got plans=%v baseline=%v", got.Plans, got.Baseline)
	}

	// meta.json should now exist with the skeleton.
	if _, err := os.Stat(filepath.Join(tlDir, MetaFileName)); err != nil {
		t.Errorf("meta.json should exist after LoadMeta salvaged group, err=%v", err)
	}

	// A subsequent LoadMeta reads meta.json (no longer touches the legacy
	// group file).
	got2, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta (second call): %v", err)
	}
	if got2.Group != "legacy-only-grp" {
		t.Errorf("Group = %q, want legacy-only-grp", got2.Group)
	}
}

func TestLoadMeta_EmptyGroupFile(t *testing.T) {
	dir := t.TempDir()
	id := "emptygroup"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)

	// group file is whitespace only.
	writeRawFile(t, filepath.Join(tlDir, "group"), []byte("  \n"))

	_, err := LoadMeta(dir, id)
	if !errors.Is(err, ErrNoGroup) {
		t.Errorf("err = %v, want ErrNoGroup", err)
	}
}

func TestLoadMeta_NoSources(t *testing.T) {
	dir := t.TempDir()
	id := "missing"
	// Create the directory but no files at all.
	if err := os.MkdirAll(filepath.Join(dir, "logs", "timelines", "l2-"+id), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := LoadMeta(dir, id)
	if !errors.Is(err, ErrNoGroup) {
		t.Errorf("err = %v, want ErrNoGroup", err)
	}
}

func TestLoadMeta_NoDir(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadMeta(dir, "no-such-session")
	if err == nil {
		t.Errorf("expected error for missing dir")
	}
}

func TestLoadMeta_SchemaVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	id := "schemamismatch"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	raw, _ := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		Group         string `json:"group"`
	}{SchemaVersion: 999, Group: "dev"})
	writeRawFile(t, filepath.Join(tlDir, MetaFileName), raw)

	_, err := LoadMeta(dir, id)
	if !errors.Is(err, ErrMetaSchemaMismatch) {
		t.Errorf("err = %v, want ErrMetaSchemaMismatch", err)
	}
}

func TestMergeAndSave_PreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	id := "merge"

	// Seed: write initial meta with several fields populated.
	seed := &SessionMeta{
		SchemaVersion: CurrentSchemaVersion,
		Group:         "dev",
		WorkDir:       "/seed",
		GitBaseRef:    "seed-ref",
		Level:         "L1",
		Plans:         []string{"plan/seed.md"},
		Baseline:      map[string]string{"a.go": "h-a"},
	}
	if err := SaveMeta(dir, id, seed); err != nil {
		t.Fatalf("SaveMeta seed: %v", err)
	}

	// Mutate only Name.
	if err := MergeAndSave(dir, id, func(m *SessionMeta) {
		m.Name = "after-merge"
	}); err != nil {
		t.Fatalf("MergeAndSave: %v", err)
	}

	got, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.Name != "after-merge" {
		t.Errorf("Name = %q, want after-merge", got.Name)
	}
	if got.Group != "dev" {
		t.Errorf("Group = %q, want dev (preserved)", got.Group)
	}
	if got.WorkDir != "/seed" {
		t.Errorf("WorkDir = %q, want /seed (preserved)", got.WorkDir)
	}
	if got.GitBaseRef != "seed-ref" {
		t.Errorf("GitBaseRef = %q, want seed-ref (preserved)", got.GitBaseRef)
	}
	if got.Level != "L1" {
		t.Errorf("Level = %q, want L1 (preserved)", got.Level)
	}
	if len(got.Plans) != 1 || got.Plans[0] != "plan/seed.md" {
		t.Errorf("Plans = %v, want [plan/seed.md] (preserved)", got.Plans)
	}
	if got.Baseline["a.go"] != "h-a" {
		t.Errorf("Baseline = %v, want h-a for a.go (preserved)", got.Baseline)
	}
}

func TestMergeAndSave_AppendPlanDedup(t *testing.T) {
	dir := t.TempDir()
	id := "plans"

	// Empty starting state: MergeAndSave must work even when meta.json is absent.
	if err := MergeAndSave(dir, id, func(m *SessionMeta) {
		m.Group = "dev"
	}); err != nil {
		t.Fatalf("first MergeAndSave: %v", err)
	}

	appendPlan := func(p string) {
		if err := MergeAndSave(dir, id, func(m *SessionMeta) {
			for _, existing := range m.Plans {
				if existing == p {
					return
				}
			}
			m.Plans = append(m.Plans, p)
		}); err != nil {
			t.Fatalf("append %s: %v", p, err)
		}
	}

	appendPlan("plan/a.md")
	appendPlan("plan/b.md")
	appendPlan("plan/a.md") // duplicate — should be a no-op.

	got, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(got.Plans) != 2 {
		t.Errorf("Plans = %v, want 2 entries (a, b)", got.Plans)
	}
}

func TestMergeAndSave_ConcurrentWritesPreserveAllFields(t *testing.T) {
	dir := t.TempDir()
	id := "concurrent"

	// Seed with full state.
	if err := SaveMeta(dir, id, &SessionMeta{
		SchemaVersion: CurrentSchemaVersion,
		Group:         "dev",
		WorkDir:       "/work",
		GitBaseRef:    "ref",
		Level:         "L1",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Concurrently mutate different fields.
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = MergeAndSave(dir, id, func(m *SessionMeta) { m.Name = "alpha" })
	}()
	go func() {
		defer wg.Done()
		_ = MergeAndSave(dir, id, func(m *SessionMeta) { m.Level = "L2" })
	}()
	go func() {
		defer wg.Done()
		_ = MergeAndSave(dir, id, func(m *SessionMeta) {
			m.Plans = append(m.Plans, "plan/x.md")
		})
	}()
	wg.Wait()

	got, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.Group != "dev" || got.WorkDir != "/work" || got.GitBaseRef != "ref" {
		t.Errorf("seed fields lost: group=%q workdir=%q ref=%q", got.Group, got.WorkDir, got.GitBaseRef)
	}
	// Each field independently may or may not win (race), but at least one
	// of {Name, Level, Plans} must reflect the concurrent write.
	hasName := got.Name == "alpha"
	hasLevel := got.Level == "L2"
	hasPlan := false
	for _, p := range got.Plans {
		if p == "plan/x.md" {
			hasPlan = true
			break
		}
	}
	if !hasName && !hasLevel && !hasPlan {
		t.Errorf("all concurrent writes lost: %+v", got)
	}
}

func TestMetaFilePath(t *testing.T) {
	got := MetaFilePath("/work", "abc")
	want := filepath.Join("/work", "logs", "timelines", "l2-abc", MetaFileName)
	if got != want {
		t.Errorf("MetaFilePath = %q, want %q", got, want)
	}
}
