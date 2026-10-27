// Package storage manages disposable history separately from conversation recovery.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger/rotating"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

const MiB int64 = 1 << 20

var ErrStale = errors.New("storage preview or configuration has changed; refresh and preview again")

type Category struct {
	ID       string `json:"id"`
	Bytes    int64  `json:"bytes"`
	Files    int    `json:"files"`
	OldestAt string `json:"oldest_at"`
	Warning  bool   `json:"warning"`
}
type LegacyFile struct {
	Name       string    `json:"name"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}
type Usage struct {
	LegacyFiles   []LegacyFile `json:"legacy_files"`
	SampledAt     time.Time    `json:"sampled_at"`
	TotalBytes    int64        `json:"total_bytes"`
	FileBytes     int64        `json:"file_bytes"`
	DatabaseBytes int64        `json:"database_bytes"`
	Categories    []Category   `json:"categories"`
	Errors        []string     `json:"errors"`
}
type CleanupResult struct {
	FinishedAt   time.Time `json:"finished_at"`
	DeletedFiles int       `json:"deleted_files"`
	FreedBytes   int64     `json:"freed_bytes"`
	Errors       []string  `json:"errors"`
}
type Preview struct {
	ID             string               `json:"preview_id"`
	Revision       string               `json:"revision"`
	ExpiresAt      time.Time            `json:"expires_at"`
	Files          int                  `json:"files"`
	Bytes          int64                `json:"bytes"`
	Categories     []Category           `json:"categories"`
	ProtectedFiles int                  `json:"protected_files"`
	Errors         []string             `json:"errors"`
	Policy         config.StorageConfig `json:"policy"`
	candidates     []candidate
	includeLegacy  bool
}
type candidate struct {
	Path, Category, ObservationID, CronID, DispatchDir string
	Size                                               int64
	Modified                                           time.Time
	Info                                               fs.FileInfo
}
type Manager struct {
	mu         sync.Mutex
	root       string
	policy     func() config.StorageConfig
	db         *db.DB
	previews   map[string]Preview
	last       CleanupResult
	next       time.Time
	unregister func()
	cancel     context.CancelFunc
	done       chan struct{}
}

func Revision(p config.StorageConfig) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func New(root string, policy func() config.StorageConfig, database *db.DB) *Manager {
	m := &Manager{root: filepath.Clean(root), policy: policy, db: database, previews: make(map[string]Preview)}
	b, err := os.ReadFile(filepath.Join(root, "logs", "storage-cleanup.json"))
	if err == nil {
		_ = json.Unmarshal(b, &m.last)
	}
	m.unregister = InstallWriterPolicies(root, policy)
	return m
}

// InstallWriterPolicies must run before startup diagnostics open their first file.
func InstallWriterPolicies(root string, policy func() config.StorageConfig) func() {
	releaseWriter := rotating.RegisterPolicy(filepath.Join(root, "logs"), func(dir, base string) rotating.Limits {
		p := policy()
		rel, _ := filepath.Rel(filepath.Join(root, "logs"), dir)
		var f config.FilePolicy
		switch {
		case strings.HasPrefix(rel, "timelines"):
			f = p.SessionHistory.FilePolicy
		case strings.HasPrefix(rel, "cron"):
			f = p.CronHistory.FilePolicy
		default:
			f = p.RuntimeLogs.FilePolicy
			if rel == "http" && p.RuntimeLogs.HTTPOverride {
				f.MaxFileMB = p.RuntimeLogs.HTTPMaxFileMB
				f.RetentionDays = p.RuntimeLogs.HTTPRetentionDays
				f.MaxFiles = p.RuntimeLogs.HTTPMaxFiles
			}
		}
		// Recovery files are cleaned at the owning execution/session boundary, never by an arbitrary writer.
		if strings.HasPrefix(rel, "cron") {
			f.RetentionDays = 0
			f.MaxFiles = 0
		}
		return rotating.Limits{MaxBytes: int64(f.MaxFileMB) * MiB, MaxDays: f.RetentionDays, MaxFiles: f.MaxFiles}
	})
	releaseDetails := observability.RegisterPolicy(root, func() observability.Policy {
		p := policy().ExecutionDetails
		return observability.Policy{MaxBytes: int64(p.MaxTotalMB) * MiB, MaxTurnBytes: int64(p.MaxTurnMB) * MiB, MaxFileBytes: int64(p.MaxFileMB) * MiB}
	})
	return func() { releaseDetails(); releaseWriter() }
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	m.next = time.Now().Add(time.Hour)
	go func() {
		defer close(m.done)
		m.periodic()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.periodic()
			}
		}
	}()
}
func (m *Manager) Close() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	if m.unregister != nil {
		m.unregister()
	}
}
func (m *Manager) Status() (CleanupResult, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last, m.next
}
func (m *Manager) periodic() {
	p := m.policy()
	preview, err := m.Preview(p, Revision(p), false)
	if err == nil && len(preview.Errors) == 0 {
		_, _ = m.Execute(preview.ID, preview.Revision, true)
	}
	m.mu.Lock()
	m.next = time.Now().Add(time.Hour)
	m.mu.Unlock()
}
func (m *Manager) Usage() Usage {
	files, errs := m.scan()
	u := Usage{SampledAt: time.Now().UTC(), Categories: []Category{}, Errors: errs, LegacyFiles: []LegacyFile{}}
	groups := map[string]*Category{}
	for _, id := range []string{"runtime_logs", "session_history", "execution_details", "cron_history", "legacy", "database", "other"} {
		groups[id] = &Category{ID: id}
	}
	for _, f := range files {
		if f.Category == "legacy" {
			u.LegacyFiles = append(u.LegacyFiles, LegacyFile{Name: filepath.Base(f.Path), Bytes: f.Size, ModifiedAt: f.Modified})
		}
		g := groups[f.Category]
		g.Bytes += f.Size
		g.Files++
		if g.OldestAt == "" || f.Modified.UTC().Format(time.RFC3339Nano) < g.OldestAt {
			g.OldestAt = f.Modified.UTC().Format(time.RFC3339Nano)
		}
		if f.Category == "database" {
			u.DatabaseBytes += f.Size
		} else {
			u.FileBytes += f.Size
		}
	}
	p := m.policy()
	for _, id := range []string{"runtime_logs", "session_history", "execution_details", "cron_history", "legacy", "database", "other"} {
		g := groups[id]
		var cap int
		switch id {
		case "runtime_logs":
			cap = p.RuntimeLogs.MaxTotalMB
		case "execution_details":
			cap = p.ExecutionDetails.MaxTotalMB
		case "cron_history":
			cap = p.CronHistory.MaxTotalMB
		}
		if cap > 0 {
			g.Warning = g.Bytes >= int64(cap)*MiB
		}
		if id == "session_history" && p.SessionHistory.WarningMB > 0 {
			g.Warning = g.Bytes >= int64(p.SessionHistory.WarningMB)*MiB
		}
		u.Categories = append(u.Categories, *g)
	}
	u.TotalBytes = u.FileBytes + u.DatabaseBytes
	return u
}
func category(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 1 && strings.HasPrefix(parts[0], "soloqueue.db") {
		return "database"
	}
	if len(parts) < 2 || parts[0] != "logs" {
		return "other"
	}
	if parts[1] == "observability" {
		return "execution_details"
	}
	if parts[1] == "cron" {
		return "cron_history"
	}
	if parts[1] == "timelines" {
		for _, p := range parts {
			if p == "delegations" {
				return "execution_details"
			}
		}
		return "session_history"
	}
	if len(parts) == 2 && (parts[1] == "stderr.log" || parts[1] == "skill_updates.log") {
		return "legacy"
	}
	if parts[1] == "system" || parts[1] == "http" || strings.HasPrefix(parts[1], "qqbot") || strings.HasPrefix(parts[1], "wechat") || strings.HasPrefix(parts[1], "wecom") || strings.HasPrefix(parts[1], "telegram") {
		return "runtime_logs"
	}
	return "other"
}
func (m *Manager) scan() ([]candidate, []string) {
	files := []candidate{}
	errs := []string{}
	root := filepath.Join(m.root, "logs")
	// WalkDir does not follow symlinks. Refuse even a symlink at the logs root.
	if st, e := os.Lstat(root); e == nil && st.Mode()&os.ModeSymlink != 0 {
		return files, []string{"logs directory is a symbolic link"}
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, "cannot read storage entry")
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			errs = append(errs, "cannot stat storage entry")
			return nil
		}
		if !st.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		if len(files) >= 100000 {
			errs = append(errs, "storage scan reached its 100000-file safety limit")
			return fs.SkipAll
		}
		files = append(files, candidate{Path: rel, Category: category(rel), Size: st.Size(), Modified: st.ModTime(), Info: st})
		return nil
	})
	for _, name := range []string{"soloqueue.db", "soloqueue.db-wal", "soloqueue.db-shm"} {
		p := filepath.Join(m.root, name)
		if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() {
			files = append(files, candidate{Path: name, Category: "database", Size: st.Size(), Modified: st.ModTime(), Info: st})
		}
	}
	return files, errs
}

var dated = regexp.MustCompile(`^(.+)-(\d{4}-\d{2}-\d{2})(?:-(\d+))?\.jsonl$`)

func dateOf(path string) (time.Time, bool) {
	match := dated.FindStringSubmatch(filepath.Base(path))
	if match == nil {
		return time.Time{}, false
	}
	t, e := time.ParseInLocation("2006-01-02", match[2], time.Local)
	return t, e == nil
}
func expired(date time.Time, days int, now time.Time) bool {
	if days <= 0 {
		return false
	}
	y, mo, d := now.Date()
	cut := time.Date(y, mo, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-days)
	return date.Before(cut)
}
func (m *Manager) safe(path string) bool {
	rel := filepath.Clean(path)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	cur := m.root
	for _, p := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, p)
		st, e := os.Lstat(cur)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func (m *Manager) Preview(policy config.StorageConfig, revision string, includeLegacy bool) (Preview, error) {
	if err := policy.Validate(); err != nil {
		return Preview{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if Revision(m.policy()) != revision {
		return Preview{}, ErrStale
	}
	files, errs := m.scan()
	now := time.Now()
	p := Preview{Revision: revision, ExpiresAt: now.Add(5 * time.Minute), Categories: []Category{}, Errors: errs, Policy: policy, includeLegacy: includeLegacy}
	selected := map[string]candidate{}
	runtimeGroups := map[string][]candidate{}
	runtimeAll := []candidate{}
	history := []candidate{}
	for _, f := range files {
		if f.Category == "legacy" {
			if includeLegacy && !rotating.IsActive(filepath.Join(m.root, f.Path)) {
				selected[f.Path] = f
			}
			continue
		}
		if f.Category == "runtime_logs" {
			if _, ok := dateOf(f.Path); !ok {
				continue
			}
			match := dated.FindStringSubmatch(filepath.Base(f.Path))
			key := filepath.Join(filepath.Dir(f.Path), match[1])
			runtimeGroups[key] = append(runtimeGroups[key], f)
			runtimeAll = append(runtimeAll, f)
		}
		if f.Category == "session_history" && strings.HasPrefix(filepath.Base(f.Path), "timeline-") {
			history = append(history, f)
		}
	}
	for key, group := range runtimeGroups {
		sortCandidates(group)
		days, count := policy.RuntimeLogs.RetentionDays, policy.RuntimeLogs.MaxFiles
		if strings.HasPrefix(filepath.ToSlash(key), "logs/http/") && policy.RuntimeLogs.HTTPOverride {
			days = policy.RuntimeLogs.HTTPRetentionDays
			count = policy.RuntimeLogs.HTTPMaxFiles
		}
		eligible := []candidate{}
		current := ""
		if len(group) > 0 {
			current = group[len(group)-1].Path
		}
		for _, f := range group {
			if rotating.IsActive(filepath.Join(m.root, f.Path)) {
				p.ProtectedFiles++
				continue
			}
			d, _ := dateOf(f.Path)
			if expired(d, days, now) {
				selected[f.Path] = f
			} else if f.Path == current {
				p.ProtectedFiles++
			} else {
				eligible = append(eligible, f)
			}
		}
		if count > 0 {
			for len(eligible) > count {
				selected[eligible[0].Path] = eligible[0]
				eligible = eligible[1:]
			}
		}
	}
	selectBudget(runtimeAll, int64(policy.RuntimeLogs.MaxTotalMB)*MiB, selected, func(f candidate) bool {
		return !rotating.IsActive(filepath.Join(m.root, f.Path))
	})
	for _, f := range history {
		if rotating.IsActive(filepath.Join(m.root, f.Path)) {
			p.ProtectedFiles++
			continue
		}
		d, ok := dateOf(f.Path)
		if ok && expired(d, policy.SessionHistory.RetentionDays, now) {
			selected[f.Path] = f
		}
	}
	details := m.dispatchUnits(files, &p)
	units, err := observability.ForWorkDir(m.root).RetentionUnits()
	if err != nil {
		p.Errors = append(p.Errors, "cannot inspect execution details")
	} else {
		for _, u := range units {
			if u.Bytes == 0 {
				continue
			}
			if u.Active {
				p.ProtectedFiles++
				continue
			}
			details = append(details, candidate{Path: "observation:" + u.ID, Category: "execution_details", ObservationID: u.ID, Size: u.Bytes, Modified: u.FinishedAt})
		}
	}
	var detailBytes int64
	for _, f := range files {
		if f.Category == "execution_details" {
			detailBytes += f.Size
		}
	}
	sort.Slice(details, func(i, j int) bool { return details[i].Modified.Before(details[j].Modified) })
	remaining := len(details)
	for _, u := range details {
		if (policy.ExecutionDetails.RetentionDays > 0 && u.Modified.Before(now.AddDate(0, 0, -policy.ExecutionDetails.RetentionDays))) || (policy.ExecutionDetails.MaxTotalMB > 0 && detailBytes > int64(policy.ExecutionDetails.MaxTotalMB)*MiB) || (policy.ExecutionDetails.MaxFiles > 0 && remaining > policy.ExecutionDetails.MaxFiles) {
			selected[u.Path] = u
			detailBytes -= u.Size
			remaining--
		}
	}
	m.selectCron(files, policy, now, selected, &p)
	groups := map[string]*Category{}
	for _, f := range selected {
		p.candidates = append(p.candidates, f)
		p.Files++
		p.Bytes += f.Size
		g := groups[f.Category]
		if g == nil {
			g = &Category{ID: f.Category}
			groups[f.Category] = g
		}
		g.Bytes += f.Size
		g.Files++
	}
	sort.Slice(p.candidates, func(i, j int) bool { return p.candidates[i].Path < p.candidates[j].Path })
	for _, id := range []string{"runtime_logs", "session_history", "execution_details", "cron_history", "legacy"} {
		if g := groups[id]; g != nil {
			p.Categories = append(p.Categories, *g)
		}
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return Preview{}, err
	}
	p.ID = hex.EncodeToString(token)
	for id, old := range m.previews {
		if now.After(old.ExpiresAt) {
			delete(m.previews, id)
		}
	}
	if len(m.previews) >= 16 {
		return Preview{}, fmt.Errorf("too many outstanding previews; wait for them to expire")
	}
	m.previews[p.ID] = p
	return p, nil
}
func sortCandidates(files []candidate) {
	sort.Slice(files, func(i, j int) bool {
		a, b := dated.FindStringSubmatch(filepath.Base(files[i].Path)), dated.FindStringSubmatch(filepath.Base(files[j].Path))
		if a != nil && b != nil {
			if a[2] != b[2] {
				return a[2] < b[2]
			}
			var n, k int
			n = 1
			k = 1
			if a[3] != "" {
				_, _ = fmt.Sscan(a[3], &n)
			}
			if b[3] != "" {
				_, _ = fmt.Sscan(b[3], &k)
			}
			return n < k
		}
		return files[i].Modified.Before(files[j].Modified)
	})
}
func latestInGroup(f candidate, groups map[string][]candidate) bool {
	match := dated.FindStringSubmatch(filepath.Base(f.Path))
	if match == nil {
		return true
	}
	g := groups[filepath.Join(filepath.Dir(f.Path), match[1])]
	return len(g) > 0 && g[len(g)-1].Path == f.Path
}
func selectBudget(files []candidate, max int64, selected map[string]candidate, eligible func(candidate) bool) {
	if max <= 0 {
		return
	}
	var total int64
	for _, f := range files {
		if _, ok := selected[f.Path]; !ok {
			total += f.Size
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified.Before(files[j].Modified) })
	for _, f := range files {
		if total <= max {
			break
		}
		if _, ok := selected[f.Path]; ok || !eligible(f) {
			continue
		}
		selected[f.Path] = f
		total -= f.Size
	}
}

type cronExecution struct {
	ID, Dir, Task string
	Finished      time.Time
}

func (m *Manager) cronExecutions() ([]cronExecution, error) {
	if m.db == nil {
		return nil, nil
	}
	rows, err := m.db.Query(`SELECT id, task_id, timeline_dir, completed_at FROM cron_execution_history WHERE status IN ('success','failed','panic') AND timeline_dir <> '' ORDER BY executed_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cronExecution{}
	for rows.Next() {
		var r cronExecution
		var ts string
		if err := rows.Scan(&r.ID, &r.Task, &r.Dir, &ts); err != nil {
			return nil, err
		}
		r.Finished, _ = time.Parse(time.RFC3339Nano, ts)
		if r.Finished.IsZero() {
			r.Finished, _ = time.Parse("2006-01-02 15:04:05", ts)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (m *Manager) selectCron(files []candidate, policy config.StorageConfig, now time.Time, selected map[string]candidate, p *Preview) {
	execs, err := m.cronExecutions()
	if err != nil {
		p.Errors = append(p.Errors, "cannot inspect scheduled execution history")
		return
	}
	seen := map[string]int{}
	all := []candidate{}
	eligible := 0
	for _, e := range execs {
		dir := filepath.Clean(e.Dir)
		if filepath.IsAbs(dir) {
			rel, err := filepath.Rel(m.root, dir)
			if err != nil {
				continue
			}
			dir = rel
		}
		if !strings.HasPrefix(filepath.ToSlash(dir), "logs/cron/") || !m.safe(dir) || !m.safeTree(dir) {
			continue
		}
		group := []candidate{}
		protected := false
		for _, f := range files {
			if strings.HasPrefix(f.Path, dir+string(filepath.Separator)) {
				f.CronID = e.ID
				group = append(group, f)
				if rotating.IsActive(filepath.Join(m.root, f.Path)) {
					protected = true
				}
				if filepath.Base(f.Path) == "meta.json" && strings.Contains(f.Path, "delegations") {
					b, err := os.ReadFile(filepath.Join(m.root, f.Path))
					var meta struct {
						Status string `json:"status"`
					}
					if err != nil || json.Unmarshal(b, &meta) != nil || (meta.Status != "completed" && meta.Status != "failed" && meta.Status != "interrupted") {
						protected = true
					}
				}
			}
		}
		if protected {
			p.ProtectedFiles += len(group)
			continue
		}
		eligible++
		seen[e.Task]++
		all = append(all, group...)
		old := policy.CronHistory.RetentionDays > 0 && !e.Finished.IsZero() && e.Finished.Before(now.AddDate(0, 0, -policy.CronHistory.RetentionDays))
		excess := policy.CronHistory.MaxPerTask > 0 && seen[e.Task] > policy.CronHistory.MaxPerTask
		if old || excess || (policy.CronHistory.MaxFiles > 0 && eligible > policy.CronHistory.MaxFiles) {
			for _, f := range group {
				selected[f.Path] = f
			}
		}
	}
	// Capacity chooses whole completed executions so history cannot become a partial replay.
	if policy.CronHistory.MaxTotalMB > 0 {
		var total int64
		for _, f := range files {
			if f.Category == "cron_history" {
				total += f.Size
			}
		}
		for _, f := range selected {
			if f.Category == "cron_history" {
				total -= f.Size
			}
		}
		for i := len(execs) - 1; i >= 0 && total > int64(policy.CronHistory.MaxTotalMB)*MiB; i-- {
			for _, f := range all {
				if f.CronID == execs[i].ID {
					if _, ok := selected[f.Path]; !ok {
						selected[f.Path] = f
						total -= f.Size
					}
				}
			}
		}
	}
}
func (m *Manager) ValidatePreview(id, revision string, policy config.StorageConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.previews[id]
	if !ok || time.Now().After(p.ExpiresAt) || p.Revision != revision || Revision(m.policy()) != revision || Revision(p.Policy) != Revision(policy) || len(p.Errors) > 0 {
		return ErrStale
	}
	return nil
}
func (m *Manager) Execute(id, revision string, confirm bool) (CleanupResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.previews[id]
	if !confirm || !ok || time.Now().After(p.ExpiresAt) || p.Revision != revision || Revision(m.policy()) != revision || len(p.Errors) > 0 {
		return CleanupResult{}, ErrStale
	}
	delete(m.previews, id)
	result := CleanupResult{Errors: []string{}}
	root, err := os.OpenRoot(m.root)
	if err != nil {
		return result, err
	}
	defer root.Close()
	cronDone := map[string]string{}
	for _, f := range p.candidates {
		if f.DispatchDir != "" {
			if !m.safe(f.DispatchDir) || !m.safeTree(f.DispatchDir) {
				result.Errors = append(result.Errors, "dispatch changed since preview; skipped")
				continue
			}
			stats, err := dispatch.CompactCompletedDetailsWithStats(filepath.Join(m.root, f.DispatchDir))
			result.DeletedFiles += stats.DeletedFiles
			result.FreedBytes += stats.FreedBytes
			if err != nil {
				result.Errors = append(result.Errors, "dispatch is active or could not be compacted")
			}
			continue
		}
		if f.ObservationID != "" {
			stats, err := observability.ForWorkDir(m.root).ExpireWithStats(f.ObservationID)
			result.DeletedFiles += stats.DeletedFiles
			result.FreedBytes += stats.FreedBytes
			if err != nil {
				result.Errors = append(result.Errors, "execution detail changed or could not be expired")
			}
			continue
		}
		if !m.safe(f.Path) || rotating.IsActive(filepath.Join(m.root, f.Path)) {
			result.Errors = append(result.Errors, "entry changed or became active; skipped")
			continue
		}
		st, err := os.Lstat(filepath.Join(m.root, f.Path))
		if err != nil || !st.Mode().IsRegular() || !os.SameFile(st, f.Info) || st.Size() != f.Size || !st.ModTime().Equal(f.Modified) {
			result.Errors = append(result.Errors, "entry changed since preview; skipped")
			continue
		}
		if f.CronID != "" {
			var status string
			if m.db == nil || m.db.QueryRow(`SELECT status FROM cron_execution_history WHERE id = ?`, f.CronID).Scan(&status) != nil || (status != "success" && status != "failed" && status != "panic") {
				result.Errors = append(result.Errors, "scheduled execution is not terminal; skipped")
				continue
			}
		}
		if err := rotating.RemoveInactive(filepath.Join(m.root, f.Path), func() error { return root.Remove(f.Path) }); err != nil {
			result.Errors = append(result.Errors, "could not remove storage entry")
			continue
		}
		result.DeletedFiles++
		result.FreedBytes += f.Size
		if f.CronID != "" {
			parts := strings.Split(filepath.ToSlash(f.Path), "/")
			if len(parts) >= 4 {
				cronDone[f.CronID] = filepath.Join(parts[:4]...)
			}
		}
	}
	for id, dir := range cronDone {
		remaining := false
		_ = filepath.WalkDir(filepath.Join(m.root, dir), func(_ string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				remaining = true
			}
			return nil
		})
		if remaining {
			continue
		}
		if m.db != nil {
			m.db.WMu.Lock()
			_, err := m.db.Exec(`UPDATE cron_execution_history SET details_expired = 1, timeline_dir = '' WHERE id = ?`, id)
			m.db.WMu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, "could not mark scheduled details expired")
			}
		}
	}
	stats, err := observability.ForWorkDir(m.root).PrunePromptsWithStats()
	result.DeletedFiles += stats.DeletedFiles
	result.FreedBytes += stats.FreedBytes
	if err != nil {
		result.Errors = append(result.Errors, "could not collect unused prompt snapshots")
	}
	result.FinishedAt = time.Now().UTC()
	m.last = result
	b, _ := json.Marshal(result)
	if err := root.Mkdir("logs", 0o700); err != nil && !os.IsExist(err) {
		result.Errors = append(result.Errors, "could not persist cleanup result")
	} else {
		tempName := filepath.Join("logs", ".storage-cleanup-"+p.ID)
		file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = file.Write(b)
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = root.Rename(tempName, filepath.Join("logs", "storage-cleanup.json"))
			}
			_ = root.Remove(tempName)
		}
		if err != nil {
			result.Errors = append(result.Errors, "could not persist cleanup result")
		}
	}
	m.last = result
	return result, nil
}

// Tightens identifies policy changes that can discard additional saved data.
func Tightens(old, new config.StorageConfig) bool {
	a := []int{old.RuntimeLogs.RetentionDays, old.RuntimeLogs.MaxFiles, old.RuntimeLogs.MaxTotalMB, effectiveHTTPDays(old), effectiveHTTPFiles(old), old.SessionHistory.RetentionDays, old.ExecutionDetails.RetentionDays, old.ExecutionDetails.MaxTotalMB, old.ExecutionDetails.MaxFiles, old.CronHistory.MaxFiles, old.CronHistory.RetentionDays, old.CronHistory.MaxTotalMB, old.CronHistory.MaxPerTask}
	b := []int{new.RuntimeLogs.RetentionDays, new.RuntimeLogs.MaxFiles, new.RuntimeLogs.MaxTotalMB, effectiveHTTPDays(new), effectiveHTTPFiles(new), new.SessionHistory.RetentionDays, new.ExecutionDetails.RetentionDays, new.ExecutionDetails.MaxTotalMB, new.ExecutionDetails.MaxFiles, new.CronHistory.MaxFiles, new.CronHistory.RetentionDays, new.CronHistory.MaxTotalMB, new.CronHistory.MaxPerTask}
	for i := range a {
		if b[i] > 0 && (a[i] == 0 || b[i] < a[i]) {
			return true
		}
	}
	return false
}

func effectiveHTTPDays(p config.StorageConfig) int {
	if p.RuntimeLogs.HTTPOverride {
		return p.RuntimeLogs.HTTPRetentionDays
	}
	return p.RuntimeLogs.RetentionDays
}
func effectiveHTTPFiles(p config.StorageConfig) int {
	if p.RuntimeLogs.HTTPOverride {
		return p.RuntimeLogs.HTTPMaxFiles
	}
	return p.RuntimeLogs.MaxFiles
}

func (m *Manager) treeBytes(rel string) int64 {
	var total int64
	_ = filepath.WalkDir(filepath.Join(m.root, rel), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			if st, e := d.Info(); e == nil {
				total += st.Size()
			}
		}
		return nil
	})
	return total
}
func (m *Manager) safeTree(rel string) bool {
	safe := true
	err := filepath.WalkDir(filepath.Join(m.root, rel), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			safe = false
		}
		return nil
	})
	return err == nil && safe
}
func (m *Manager) dispatchUnits(files []candidate, preview *Preview) []candidate {
	dirs := map[string][]candidate{}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f.Path), "/delegations/") && strings.HasPrefix(filepath.Base(filepath.Dir(f.Path)), "dlg_") {
			dir := filepath.Dir(f.Path)
			dirs[dir] = append(dirs[dir], f)
		}
	}
	units := []candidate{}
	for dir, group := range dirs {
		if !m.safe(dir) || !m.safeTree(dir) {
			preview.ProtectedFiles++
			continue
		}
		path := filepath.Join(m.root, dir, "meta.json")
		st, err := os.Lstat(path)
		if err != nil || st.Size() > 1<<20 {
			preview.ProtectedFiles++
			continue
		}
		b, err := os.ReadFile(path)
		var rec dispatch.Record
		if err != nil || json.Unmarshal(b, &rec) != nil {
			preview.Errors = append(preview.Errors, "cannot inspect dispatch recovery metadata")
			continue
		}
		if rec.Status != dispatch.StatusCompleted && rec.Status != dispatch.StatusFailed && rec.Status != dispatch.StatusInterrupted {
			preview.ProtectedFiles++
			continue
		}
		if rec.DetailsExpired {
			continue
		}
		var size int64
		for _, f := range group {
			if strings.HasPrefix(filepath.Base(f.Path), "stream-") {
				size += f.Size
			}
		}
		units = append(units, candidate{Path: "dispatch:" + dir, DispatchDir: dir, Category: "execution_details", Size: size, Modified: rec.UpdatedAt})
	}
	return units
}
