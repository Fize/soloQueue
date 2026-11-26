// Package storagepolicy is the single accounting implementation for the work
// directory: one classifier, one usage ledger, one deletion plan, one executor.
// Writers and the periodic cleaner both go through it, so the two can never
// disagree about how much a category occupies or which unit is expendable.
package storagepolicy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Category string

const (
	RuntimeLogs      Category = "runtime_logs"
	SessionHistory   Category = "session_history"
	ExecutionDetails Category = "execution_details"
	CronHistory      Category = "cron_history"
	Legacy           Category = "legacy"
	Database         Category = "database"
	Other            Category = "other"
)

const MiB = int64(1 << 20)

// Policy mirrors the storage configuration. It is a plain value so this package
// stays dependency-free and every importer (writer, cleaner, settings) can hand
// it the same numbers without owning a second interpretation of them.
type FilePolicy struct {
	MaxFileBytes  int64
	RetentionDays int
	MaxFiles      int
	MaxTotalBytes int64
}

type RuntimeLogPolicy struct {
	FilePolicy
	HTTPOverride      bool
	HTTPMaxFileBytes  int64
	HTTPRetentionDays int
	HTTPMaxFiles      int
}

type HistoryPolicy struct {
	FilePolicy
	WarningBytes int64
}

type DetailPolicy struct {
	FilePolicy
	MaxTurnBytes int64
}

type CronHistoryPolicy struct {
	FilePolicy
	MaxPerTask int
}

type Policy struct {
	RuntimeLogs      RuntimeLogPolicy
	SessionHistory   HistoryPolicy
	ExecutionDetails DetailPolicy
	CronHistory      CronHistoryPolicy
}

func DefaultPolicy() Policy {
	return Policy{
		RuntimeLogs:      RuntimeLogPolicy{FilePolicy: FilePolicy{MaxFileBytes: 50 * MiB, RetentionDays: 15, MaxFiles: 30, MaxTotalBytes: 1024 * MiB}, HTTPMaxFileBytes: 50 * MiB, HTTPRetentionDays: 15, HTTPMaxFiles: 30},
		SessionHistory:   HistoryPolicy{FilePolicy: FilePolicy{MaxFileBytes: 50 * MiB}, WarningBytes: 1024 * MiB},
		ExecutionDetails: DetailPolicy{FilePolicy: FilePolicy{MaxFileBytes: 50 * MiB, RetentionDays: 30, MaxTotalBytes: 1024 * MiB}, MaxTurnBytes: 50 * MiB},
		CronHistory:      CronHistoryPolicy{FilePolicy: FilePolicy{MaxFileBytes: 50 * MiB, RetentionDays: 30, MaxTotalBytes: 512 * MiB}},
	}
}

// HeadroomPercent is how far below a category limit a prune must land. Without
// headroom the next write crosses the limit again immediately.
const HeadroomPercent = 90

// RetentionFallbackFactor bounds how long expired data may survive while the
// category is still under its cap: a unit is only dropped on age once it is
// older than the configured retention multiplied by this factor. Capacity is
// the hard limit; the retention window is a fallback, not a trigger.
const RetentionFallbackFactor = 2

// Classify maps a path relative to the work directory to its storage category.
// It is the only classifier in the code base.
func Classify(rel string) Category {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 1 && strings.HasPrefix(parts[0], "soloqueue.db") {
		return Database
	}
	if len(parts) < 2 || parts[0] != "logs" {
		return Other
	}
	switch parts[1] {
	case "observability":
		return ExecutionDetails
	case "cron":
		return CronHistory
	case "timelines":
		for _, p := range parts {
			if p == "delegations" {
				return ExecutionDetails
			}
		}
		return SessionHistory
	}
	if len(parts) == 2 && (parts[1] == "stderr.log" || parts[1] == "skill_updates.log") {
		return Legacy
	}
	if parts[1] == "system" || parts[1] == "http" || strings.HasPrefix(parts[1], "qqbot") || strings.HasPrefix(parts[1], "wechat") || strings.HasPrefix(parts[1], "wecom") || strings.HasPrefix(parts[1], "telegram") {
		return RuntimeLogs
	}
	return Other
}

// Limits are the configured bounds for one category. Zero means unlimited.
type Limits struct {
	MaxBytes     int64
	MaxTurnBytes int64
	MaxFileBytes int64
	MaxFiles     int
	MaxDays      int
	MaxPerTask   int
}

// Target is the byte level a prune must reach: the limit minus headroom.
func (l Limits) Target() int64 {
	if l.MaxBytes <= 0 {
		return 0
	}
	return l.MaxBytes / 100 * HeadroomPercent
}

// LimitsFor reads one category's bounds from the configuration.
func LimitsFor(cfg Policy, c Category) Limits {
	switch c {
	case RuntimeLogs:
		p := cfg.RuntimeLogs
		return Limits{MaxBytes: p.MaxTotalBytes, MaxFileBytes: p.MaxFileBytes, MaxFiles: p.MaxFiles, MaxDays: p.RetentionDays}
	case SessionHistory:
		p := cfg.SessionHistory
		return Limits{MaxBytes: p.MaxTotalBytes, MaxFileBytes: p.MaxFileBytes, MaxFiles: p.MaxFiles, MaxDays: p.RetentionDays}
	case ExecutionDetails:
		p := cfg.ExecutionDetails
		return Limits{MaxBytes: p.MaxTotalBytes, MaxTurnBytes: p.MaxTurnBytes, MaxFileBytes: p.MaxFileBytes, MaxFiles: p.MaxFiles, MaxDays: p.RetentionDays}
	case CronHistory:
		p := cfg.CronHistory
		return Limits{MaxBytes: p.MaxTotalBytes, MaxFileBytes: p.MaxFileBytes, MaxFiles: p.MaxFiles, MaxDays: p.RetentionDays, MaxPerTask: p.MaxPerTask}
	}
	return Limits{}
}

// FileLimits is the rolling policy for one path, so the log writer stops
// re-deriving it from the configuration on its own.
func FileLimits(cfg Policy, rel string) Limits {
	l := LimitsFor(cfg, Classify(rel))
	if Classify(rel) == RuntimeLogs && strings.HasPrefix(filepath.ToSlash(rel), "logs/http/") && cfg.RuntimeLogs.HTTPOverride {
		l.MaxFileBytes = cfg.RuntimeLogs.HTTPMaxFileBytes
		l.MaxDays = cfg.RuntimeLogs.HTTPRetentionDays
		l.MaxFiles = cfg.RuntimeLogs.HTTPMaxFiles
	}
	return l
}

// Unit is the smallest deletable thing. Everything a category owns belongs to
// exactly one unit, so totals and deletions cannot drift apart.
type Unit struct {
	Key       string
	Category  Category
	Paths     []string
	Bytes     int64
	Files     int
	Modified  time.Time
	Active    bool
	Prompts   []string
	TaskKey   string
	RequestID string
}

type PromptFile struct {
	Path     string
	Bytes    int64
	Modified time.Time
}

type Ledger struct {
	Root    string
	Units   []Unit
	Prompts map[string]PromptFile
	Errors  []string
}

type Plan struct {
	Delete []Unit
	Freed  int64
	Usage  map[Category]int64
	Errors []string
}

func (p Plan) Empty() bool { return len(p.Delete) == 0 }

// Usage sums the ledger per category, prompt snapshots included.
func (l Ledger) Usage() map[Category]int64 {
	out := map[Category]int64{}
	for _, u := range l.Units {
		out[u.Category] += u.Bytes
	}
	for _, p := range l.Prompts {
		out[ExecutionDetails] += p.Bytes
	}
	return out
}

// TurnUsage is what one turn occupies: its own detail directory, the
// delegations it spawned and the snapshots it references, counted once however
// many other requests share them. When a generation is given, only that
// generation's detail directory counts; earlier generations are archived for
// their aliases and never charged to the running turn.
func (l Ledger) TurnUsage(requestID string, generation ...string) int64 {
	if requestID == "" {
		return 0
	}
	wantKey := ""
	if len(generation) > 0 && generation[0] != "" {
		wantKey = "observability/requests/" + generation[0]
	}
	var total int64
	seen := map[string]bool{}
	for _, u := range l.Units {
		if u.RequestID != requestID {
			continue
		}
		if strings.HasPrefix(u.Key, "observability/requests/") && wantKey != "" && u.Key != wantKey {
			continue
		}
		total += u.Bytes
		for _, hash := range u.Prompts {
			if seen[hash] {
				continue
			}
			seen[hash] = true
			total += l.Prompts[hash].Bytes
		}
	}
	return total
}

// Scan walks the work directory once and builds the ledger.
func Scan(root string) Ledger {
	l := Ledger{Root: root, Prompts: map[string]PromptFile{}}
	grouped := map[string]*Unit{}
	add := func(key, rel string, size int64, modified time.Time) {
		u := grouped[key]
		if u == nil {
			u = &Unit{Key: key, Category: Classify(rel)}
			grouped[key] = u
		}
		u.Paths = append(u.Paths, rel)
		u.Bytes += size
		u.Files++
		if modified.After(u.Modified) {
			u.Modified = modified
		}
	}
	_ = filepath.WalkDir(filepath.Join(root, "logs"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				l.Errors = append(l.Errors, "cannot read storage entry")
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			l.Errors = append(l.Errors, "cannot stat storage entry")
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		key, prompt := unitKey(rel)
		if prompt != "" {
			l.Prompts[prompt] = PromptFile{Path: rel, Bytes: info.Size(), Modified: info.ModTime()}
			return nil
		}
		add(key, rel, info.Size(), info.ModTime())
		return nil
	})
	for _, name := range []string{"soloqueue.db", "soloqueue.db-wal", "soloqueue.db-shm"} {
		if st, e := os.Lstat(filepath.Join(root, name)); e == nil && st.Mode().IsRegular() {
			add("database/"+name, name, st.Size(), st.ModTime())
		}
	}
	for key, u := range grouped {
		u.Key = key
		u.Prompts = promptRefs(root, *u)
		u.Active = unitActive(root, *u)
		u.TaskKey = unitTask(*u)
		u.RequestID = unitRequestID(root, *u)
		if unreadableMeta(root, *u) {
			l.Errors = append(l.Errors, "unreadable dispatch metadata")
		}
		l.Units = append(l.Units, *u)
	}
	sort.Slice(l.Units, func(i, j int) bool {
		if l.Units[i].Modified.Equal(l.Units[j].Modified) {
			return l.Units[i].Key < l.Units[j].Key
		}
		return l.Units[i].Modified.Before(l.Units[j].Modified)
	})
	return l
}

// unitKey groups a path into the unit that owns it. A non-empty prompt hash
// means the file is a shared snapshot owned by no single unit.
func unitKey(rel string) (string, string) {
	parts := strings.Split(rel, "/")
	if len(parts) >= 3 && parts[0] == "logs" {
		switch parts[1] {
		case "observability":
			if parts[2] == "requests" && len(parts) >= 4 {
				return "observability/requests/" + parts[3], ""
			}
			if parts[2] == "prompts" && len(parts) >= 4 {
				return "", strings.TrimSuffix(parts[3], ".json")
			}
		case "cron":
			if len(parts) >= 4 {
				return "cron/" + parts[2], ""
			}
		case "timelines":
			for i, p := range parts {
				if p == "delegations" && i+1 < len(parts) {
					return "delegations/" + strings.Join(parts[2:i+2], "/"), ""
				}
			}
		}
	}
	return "file/" + rel, ""
}

// unitActive protects anything the running process may still append to.
func unitActive(root string, u Unit) bool {
	switch {
	case strings.HasPrefix(u.Key, "observability/requests/"):
		var meta struct {
			Status     string     `json:"status"`
			FinishedAt *time.Time `json:"finished_at"`
		}
		path := filepath.Join(root, "logs/observability/requests", strings.TrimPrefix(u.Key, "observability/requests/"), "meta.json")
		if !metaPresent(path) {
			return false
		}
		if readJSON(path, &meta) {
			return meta.FinishedAt == nil && (meta.Status == "running" || meta.Status == "queued")
		}
		return true
	case strings.HasPrefix(u.Key, "delegations/"):
		var meta struct {
			Status string `json:"status"`
		}
		path := filepath.Join(root, "logs/timelines", strings.TrimPrefix(u.Key, "delegations/"), "meta.json")
		if !metaPresent(path) {
			return false
		}
		if readJSON(path, &meta) {
			return meta.Status == "" || meta.Status == "running" || meta.Status == "pending"
		}
		return true
	case u.Category == SessionHistory || u.Category == CronHistory || u.Category == RuntimeLogs:
		now := time.Now()
		return u.Modified.Year() == now.Year() && u.Modified.YearDay() == now.YearDay()
	}
	return false
}

// unreadableMeta reports metadata that exists but cannot be parsed. Budgets fail
// closed on it instead of guessing how much the unit occupies.
func unreadableMeta(root string, u Unit) bool {
	var path string
	switch {
	case strings.HasPrefix(u.Key, "observability/requests/"):
		path = filepath.Join(root, "logs/observability/requests", strings.TrimPrefix(u.Key, "observability/requests/"), "meta.json")
	case strings.HasPrefix(u.Key, "delegations/"):
		path = filepath.Join(root, "logs/timelines", strings.TrimPrefix(u.Key, "delegations/"), "meta.json")
	default:
		return false
	}
	var raw map[string]any
	return metaPresent(path) && !readJSON(path, &raw)
}

// unitRequestID reads the request a unit belongs to, so per-turn budgets match
// on the request rather than on the directory name.
func unitRequestID(root string, u Unit) string {
	var path string
	switch {
	case strings.HasPrefix(u.Key, "observability/requests/"):
		path = filepath.Join(root, "logs/observability/requests", strings.TrimPrefix(u.Key, "observability/requests/"), "meta.json")
	case strings.HasPrefix(u.Key, "delegations/"):
		path = filepath.Join(root, "logs/timelines", strings.TrimPrefix(u.Key, "delegations/"), "meta.json")
	default:
		return ""
	}
	var meta struct {
		RequestID string `json:"request_id"`
	}
	if !readJSON(path, &meta) {
		return ""
	}
	return meta.RequestID
}

func unitTask(u Unit) string {
	if u.Category != CronHistory {
		return ""
	}
	return strings.TrimPrefix(u.Key, "cron/")
}

// metaPresent separates a unit with no metadata (an orphan, safe to prune) from
// one whose metadata exists but cannot be read (kept until a human looks).
func metaPresent(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readJSON(path string, target any) bool {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 1<<20 {
		return false
	}
	return json.Unmarshal(b, target) == nil
}

// promptRefs lists the shared snapshots one unit references.
func promptRefs(root string, u Unit) []string {
	if u.Category != ExecutionDetails {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, rel := range u.Paths {
		if !strings.HasSuffix(rel, ".json") || !strings.Contains(rel, "/calls/") {
			continue
		}
		var call struct {
			Hash string `json:"hash"`
		}
		if !readJSON(filepath.Join(root, rel), &call) || len(call.Hash) != 64 || seen[call.Hash] {
			continue
		}
		seen[call.Hash] = true
		out = append(out, call.Hash)
	}
	return out
}

// Build decides what to delete. It is a pure function of the configuration, the
// ledger and the clock, shared by writers and the periodic cleaner.
func Build(cfg Policy, l Ledger, now time.Time) Plan {
	p := Plan{Usage: l.Usage(), Errors: append([]string{}, l.Errors...)}
	byCategory := map[Category][]Unit{}
	for _, u := range l.Units {
		switch u.Category {
		case Database, Other, Legacy:
			continue
		}
		byCategory[u.Category] = append(byCategory[u.Category], u)
	}
	refs := map[string]int{}
	for _, u := range l.Units {
		for _, hash := range u.Prompts {
			refs[hash]++
		}
	}
	selected := map[string]bool{}
	take := func(u Unit) {
		if u.Active || selected[u.Key] {
			return
		}
		selected[u.Key] = true
		p.Delete = append(p.Delete, u)
		p.Freed += u.Bytes
		p.Usage[u.Category] -= u.Bytes
		for _, hash := range u.Prompts {
			refs[hash]--
			if refs[hash] > 0 {
				continue
			}
			if file, ok := l.Prompts[hash]; ok {
				p.Freed += file.Bytes
				p.Usage[ExecutionDetails] -= file.Bytes
				delete(l.Prompts, hash)
			}
		}
	}
	for category, units := range byCategory {
		limits := LimitsFor(cfg, category)
		if limits.MaxBytes > 0 {
			target := limits.Target()
			for _, u := range units {
				if p.Usage[category] <= target {
					break
				}
				take(u)
			}
		}
		if limits.MaxPerTask > 0 {
			perTask := map[string][]Unit{}
			for _, u := range units {
				if !selected[u.Key] {
					perTask[u.TaskKey] = append(perTask[u.TaskKey], u)
				}
			}
			for _, group := range perTask {
				for i := 0; i < len(group)-limits.MaxPerTask; i++ {
					take(group[i])
				}
			}
		}
		if limits.MaxFiles > 0 {
			remaining := 0
			for _, u := range units {
				if !selected[u.Key] {
					remaining++
				}
			}
			for _, u := range units {
				if remaining <= limits.MaxFiles {
					break
				}
				if selected[u.Key] {
					continue
				}
				take(u)
				remaining--
			}
		}
		// Fallback: age alone never frees space that is still within budget, but
		// nothing outlives the retention window by more than the fallback factor.
		if limits.MaxDays > 0 {
			for _, u := range units {
				if expired(u.Modified, limits.MaxDays*RetentionFallbackFactor, now) {
					take(u)
				}
			}
		}
	}
	return p
}

func expired(at time.Time, days int, now time.Time) bool {
	if days <= 0 {
		return false
	}
	y, mo, d := now.Date()
	cut := time.Date(y, mo, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-days)
	return at.Before(cut)
}

// Result reports what an apply actually removed.
type Result struct {
	DeletedUnits  int
	DeletedFiles  int
	DeletedPrompt int
	FreedBytes    int64
}

// maxUnitsPerApply bounds one pass so a write never waits for a full reaping.
const maxUnitsPerApply = 200

// Apply removes the planned units, then the snapshots nothing references any
// more. It refuses any path outside the work directory.
func Apply(root string, p Plan) (Result, error) {
	var res Result
	var errs []error
	for i, u := range p.Delete {
		if i >= maxUnitsPerApply {
			break
		}
		if u.Active {
			continue
		}
		deleted := 0
		for _, rel := range u.Paths {
			full, ok := safePath(root, rel)
			if !ok {
				errs = append(errs, errors.New("unsafe storage path: "+rel))
				continue
			}
			st, e := os.Lstat(full)
			if e != nil || st.IsDir() {
				continue
			}
			if e := os.Remove(full); e != nil {
				errs = append(errs, e)
				continue
			}
			deleted++
			res.FreedBytes += st.Size()
		}
		pruneUnitDirs(root, u.Paths)
		if deleted > 0 {
			res.DeletedUnits++
			res.DeletedFiles += deleted
		}
	}
	if res.DeletedUnits > 0 {
		prompts, freed := orphanPrompts(root)
		res.DeletedPrompt = prompts
		res.FreedBytes += freed
	}
	return res, errors.Join(errs...)
}

// Ensure prunes one category when the ledger plus the pending write would cross
// the configured limit. Writers call it before saving detail data.
func Ensure(root string, cfg Policy, category Category, addition int64) (Result, error) {
	return EnsureKeeping(root, cfg, category, addition, nil)
}

// EnsureKeeping prunes one category while keeping the units the caller still
// needs. A writer must never delete the turn it is writing, and a turn keeps
// every generation it can still be read through.
func EnsureKeeping(root string, cfg Policy, category Category, addition int64, keep func(Unit) bool) (Result, error) {
	limits := LimitsFor(cfg, category)
	if limits.MaxBytes <= 0 {
		return Result{}, nil
	}
	l := Scan(root)
	if l.Usage()[category]+addition <= limits.MaxBytes {
		return Result{}, nil
	}
	plan := Build(cfg, l, time.Now())
	if keep != nil {
		kept := plan.Delete[:0]
		for _, u := range plan.Delete {
			if !keep(u) {
				kept = append(kept, u)
			}
		}
		plan.Delete = kept
	}
	return Apply(root, plan)
}

func safePath(root, rel string) (string, bool) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	absRoot, e1 := filepath.Abs(root)
	absFull, e2 := filepath.Abs(full)
	if e1 != nil || e2 != nil {
		return "", false
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// pruneUnitDirs removes the now-empty directories a deleted unit owned. It never
// climbs above three path segments, so category containers such as logs/system
// stay in place.
func pruneUnitDirs(root string, paths []string) {
	seen := map[string]bool{}
	var dirs []string
	for _, rel := range paths {
		for dir := filepath.Dir(rel); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
			if seen[dir] {
				break
			}
			seen[dir] = true
			if len(strings.Split(dir, "/")) >= 3 {
				dirs = append(dirs, dir)
			}
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, rel := range dirs {
		full, ok := safePath(root, rel)
		if !ok {
			continue
		}
		_ = os.Remove(full)
	}
}

// orphanPrompts frees snapshots no surviving call references.
func orphanPrompts(root string) (int, int64) {
	referenced := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(root, "logs/observability/requests"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		var call struct {
			Hash string `json:"hash"`
		}
		if readJSON(path, &call) && len(call.Hash) == 64 {
			referenced[call.Hash] = true
		}
		return nil
	})
	dir := filepath.Join(root, "logs/observability/prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var count int
	var freed int64
	for _, en := range entries {
		if en.IsDir() || referenced[strings.TrimSuffix(en.Name(), ".json")] {
			continue
		}
		info, e := en.Info()
		if e != nil {
			continue
		}
		if e := os.Remove(filepath.Join(dir, en.Name())); e != nil {
			continue
		}
		count++
		freed += info.Size()
	}
	return count, freed
}
