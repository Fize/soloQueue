package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MetaFileName is the single on-disk file that holds all persisted metadata
// for an L2 session. The legacy `meta`, `level`, `baseline`, and `group`
// files are no longer read or written by the server — the one-shot migration
// script (scripts/migrate-l2-meta) converted them to meta.json in place.
const MetaFileName = "meta.json"

// CurrentSchemaVersion is the schema version written by this build of the
// server. A persisted meta.json with a different SchemaVersion is treated as
// unsupported — LoadMeta returns an error.
const CurrentSchemaVersion = 1

// SessionMeta is the single source of truth for L2 session metadata persisted
// to disk. All fields are best-effort: a missing field is treated as the
// zero value when the file is re-read.
type SessionMeta struct {
	SchemaVersion int               `json:"schema_version"`
	Name          string            `json:"name,omitempty"`
	Group         string            `json:"group"`
	WorkDir       string            `json:"work_dir,omitempty"`
	GitBaseRef    string            `json:"git_base_ref,omitempty"`
	Plans         []string          `json:"plans,omitempty"`
	Level         string            `json:"level,omitempty"`
	Baseline      map[string]string `json:"baseline,omitempty"` // path→sha256, non-git projects only
	CtxwinUsed    int               `json:"ctxwin_used,omitempty"`
	CtxwinLimit   int               `json:"ctxwin_limit,omitempty"`
	CtxwinUpdated time.Time         `json:"ctxwin_updated_at,omitempty"`
	Usage         SessionUsage      `json:"usage,omitempty"`
}

// SessionUsage stores lifetime token totals and compact-delimited cache phases.
// Rows without reported cache details increase token totals but do not affect
// the cache hit ratio denominator.
type SessionUsage struct {
	CurrentSegment int            `json:"current_segment"`
	Lifetime       UsageTotals    `json:"lifetime"`
	Segments       []UsageSegment `json:"segments,omitempty"`
}

type UsageTotals struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheHitTokens   int `json:"cache_hit_tokens"`
	CacheMissTokens  int `json:"cache_miss_tokens"`
	CacheInputTokens int `json:"cache_input_tokens"`
}

type UsageSegment struct {
	Index  int         `json:"index"`
	Totals UsageTotals `json:"totals"`
}

func (u UsageTotals) CacheHitRatio() float64 {
	if u.CacheInputTokens <= 0 {
		return 0
	}
	return float64(u.CacheHitTokens) / float64(u.CacheInputTokens)
}

func (u *SessionUsage) add(segment int, input, output, total, hit, miss int, cacheReported bool) {
	if u == nil {
		return
	}
	if segment < 0 {
		segment = 0
	}
	for len(u.Segments) <= segment {
		u.Segments = append(u.Segments, UsageSegment{Index: len(u.Segments)})
	}
	addUsageTotals(&u.Lifetime, input, output, total, hit, miss, cacheReported)
	addUsageTotals(&u.Segments[segment].Totals, input, output, total, hit, miss, cacheReported)
	if segment > u.CurrentSegment {
		u.CurrentSegment = segment
	}
}

func addUsageTotals(t *UsageTotals, input, output, total, hit, miss int, cacheReported bool) {
	t.InputTokens += input
	t.OutputTokens += output
	t.TotalTokens += total
	if cacheReported {
		t.CacheHitTokens += hit
		t.CacheMissTokens += miss
		t.CacheInputTokens += input
	}
}

// ErrMetaSchemaMismatch is returned when meta.json exists with a different
// schema_version than the current build supports.
var ErrMetaSchemaMismatch = errors.New("session meta schema version mismatch")

// ErrNoGroup is returned when a session has no group on disk and cannot be
// restored. This matches the historical behaviour where sessions missing a
// group were considered unrestorable.
var ErrNoGroup = errors.New("session has no group")

// metaMu serialises read-modify-write of meta.json across the process so that
// concurrent SaveMeta / MergeAndSave calls cannot interleave their read of the
// old blob with their write of the new blob. L2SessionStore.mu provides
// in-process serialisation at the store level; metaMu covers the file-level
// race when other code paths (HTTP handlers) write directly.
var metaMu sync.Mutex

// MetaFilePath returns the path to meta.json for an L2 session.
func MetaFilePath(workDir, id string) string {
	return filepath.Join(workDir, "logs", "timelines", "l2-"+id, MetaFileName)
}

// LoadMeta loads the persisted metadata for an L2 session.
//
// If meta.json is present and parses cleanly, it is returned. If meta.json is
// missing and the directory still has a legacy `group` file, a minimal
// SessionMeta is generated (group is taken from that file) and persisted to
// meta.json. All other legacy files (`meta`, `level`, `baseline`) are ignored
// — the one-shot migration script is responsible for their content.
//
// Returns ErrNoGroup when no group can be recovered. Returns
// ErrMetaSchemaMismatch when meta.json is present but has a different
// schema_version.
func LoadMeta(workDir, id string) (*SessionMeta, error) {
	tlDir := filepath.Join(workDir, "logs", "timelines", "l2-"+id)
	if info, err := os.Stat(tlDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("L2 session %q timeline directory not found", id)
	}

	metaPath := filepath.Join(tlDir, MetaFileName)
	if data, err := os.ReadFile(metaPath); err == nil {
		var m SessionMeta
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			return nil, fmt.Errorf("parse %s: %w", MetaFileName, jerr)
		}
		if m.SchemaVersion == 0 {
			m.SchemaVersion = CurrentSchemaVersion
		}
		if m.SchemaVersion != CurrentSchemaVersion {
			return nil, fmt.Errorf("%w: persisted=%d current=%d",
				ErrMetaSchemaMismatch, m.SchemaVersion, CurrentSchemaVersion)
		}
		if m.Group == "" {
			return nil, ErrNoGroup
		}
		return &m, nil
	}

	// meta.json absent: try to salvage the group from the legacy `group` file
	// and persist a minimal skeleton. The other legacy files (meta, level,
	// baseline) are deliberately not consulted — their content is rebuilt by
	// the next session activity (level from the router, baseline only matters
	// if the user re-opens the Changes tab and the snapshot is stale, which
	// is acceptable for pre-migration sessions).
	groupPath := filepath.Join(tlDir, "group")
	if data, err := os.ReadFile(groupPath); err == nil {
		group := strings.TrimSpace(string(data))
		if group == "" {
			return nil, ErrNoGroup
		}
		skeleton := &SessionMeta{
			SchemaVersion: CurrentSchemaVersion,
			Group:         group,
		}
		if saveErr := SaveMeta(workDir, id, skeleton); saveErr != nil {
			// Even if persist fails, return the in-memory skeleton so the
			// caller can decide; the next call will retry.
			return skeleton, nil
		}
		return skeleton, nil
	}

	return nil, ErrNoGroup
}

// SaveMeta atomically writes the meta.json file for an L2 session. The write
// is performed via temp-file + rename so readers never observe a half-written
// file.
func SaveMeta(workDir, id string, m *SessionMeta) error {
	if m == nil {
		return errors.New("SaveMeta: nil meta")
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = CurrentSchemaVersion
	}

	metaPath := MetaFilePath(workDir, id)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", metaPath, err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", MetaFileName, err)
	}
	data = append(data, '\n')

	metaMu.Lock()
	defer metaMu.Unlock()

	tmp := metaPath + ".tmp." + strconv.Itoa(os.Getpid())
	return atomicWriteFile(metaPath, tmp, data, 0o644)
}

// MergeAndSave reads the current meta.json, applies `mutate` to it, and
// writes the result back atomically. If the file is missing the merge starts
// from a zero-value SessionMeta (with SchemaVersion set). The caller is
// responsible for holding any in-process lock that prevents concurrent
// mutations of the same session.
func MergeAndSave(workDir, id string, mutate func(*SessionMeta)) error {
	return MergeAndSavePath(MetaFilePath(workDir, id), mutate)
}

// LoadMetaPath reads a meta.json by path for the L1 timeline, whose metadata
// lives beside the default timeline rather than in an l2-<id> directory.
func LoadMetaPath(metaPath string) (*SessionMeta, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var m SessionMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", MetaFileName, err)
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = CurrentSchemaVersion
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: persisted=%d current=%d", ErrMetaSchemaMismatch, m.SchemaVersion, CurrentSchemaVersion)
	}
	return &m, nil
}

// MergeAndSavePath atomically updates the metadata file at metaPath. Caller
// serializes mutations for a given session.
func MergeAndSavePath(metaPath string, mutate func(*SessionMeta)) error {
	if mutate == nil {
		return errors.New("MergeAndSavePath: nil mutate")
	}

	metaMu.Lock()
	defer metaMu.Unlock()

	m, err := LoadMetaPath(metaPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if m == nil {
		m = &SessionMeta{SchemaVersion: CurrentSchemaVersion}
	}

	mutate(m)
	if m.SchemaVersion == 0 {
		m.SchemaVersion = CurrentSchemaVersion
	}

	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", metaPath, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", MetaFileName, err)
	}
	data = append(data, '\n')

	tmp := metaPath + ".tmp." + strconv.Itoa(os.Getpid())
	return atomicWriteFile(metaPath, tmp, data, 0o644)
}

// readMetaLocked must be called with metaMu held. It is a private variant of
// LoadMeta that reads only meta.json and returns ErrNoGroup when the file is
// absent.
func readMetaLocked(workDir, id string) (*SessionMeta, error) {
	metaPath := MetaFilePath(workDir, id)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoGroup
		}
		return nil, fmt.Errorf("read %s: %w", MetaFileName, err)
	}
	var m SessionMeta
	if jerr := json.Unmarshal(data, &m); jerr != nil {
		return nil, fmt.Errorf("parse %s: %w", MetaFileName, jerr)
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = CurrentSchemaVersion
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: persisted=%d current=%d",
			ErrMetaSchemaMismatch, m.SchemaVersion, CurrentSchemaVersion)
	}
	return &m, nil
}

// atomicWriteFile writes data to a sibling temp file, fsyncs it, then renames
// it over target. This guarantees that a concurrent reader sees either the
// old contents or the full new contents — never a partial write.
func atomicWriteFile(target, tmp string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open temp %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write temp %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync temp %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, target, err)
	}
	// Best-effort fsync of the directory so the rename is durable.
	if dir, derr := os.Open(filepath.Dir(target)); derr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
