package server

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

const maxChangeRevisionBytes int64 = 1 << 20

var errChangeRevisionConflict = errors.New("change revision conflict")

// ─── Response Types ──────────────────────────────────────────────────────────

// FileChange represents a single changed file in the session.
type FileChange struct {
	Path      string     `json:"path"`                 // relative path
	Status    string     `json:"status"`               // "added" | "modified" | "deleted" | "renamed"
	OldPath   string     `json:"old_path,omitempty"`   // for renames
	Additions int        `json:"additions"`            // added line count
	Deletions int        `json:"deletions"`            // deleted line count
	Binary    bool       `json:"binary"`               // true if binary file
	SizeBytes int64      `json:"size_bytes,omitempty"` // file size (for binary files)
	Revision  string     `json:"revision"`             // stable identifier for this file version
	Hunks     []DiffHunk `json:"hunks,omitempty"`      // line-level diff (text files only)
}

// DiffHunk represents a contiguous block of diff lines.
type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine represents a single line in a diff hunk.
type DiffLine struct {
	Type    string `json:"type"` // "add" | "del" | "ctx"
	Content string `json:"content"`
	OldNum  int    `json:"old_num,omitempty"` // 1-indexed old line number
	NewNum  int    `json:"new_num,omitempty"` // 1-indexed new line number
}

// ChangesResponse is the unified response for the session changes endpoint.
type ChangesResponse struct {
	Changes        []FileChange `json:"changes"`
	TotalAdditions int          `json:"total_additions"`
	TotalDeletions int          `json:"total_deletions"`
	IsGitRepo      bool         `json:"is_git_repo"`
	BaseRef        string       `json:"base_ref,omitempty"` // git commit hash (git repos only)
	Plans          []string     `json:"plans,omitempty"`
}

// ─── Handler ─────────────────────────────────────────────────────────────────

// handleGetSessionChanges returns the file changes made during an L2 session.
//
// For git repos: uses `git diff <base_ref>` where base_ref is the HEAD commit
// captured at session start.
//
// For non-git repos: compares the current filesystem against a file-hash
// snapshot taken at session start, producing line-level diffs for changed text files.
func (m *Mux) handleGetSessionChanges(w http.ResponseWriter, r *http.Request) {
	if m.l2Store == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
		return
	}

	id := chi.URLParam(r, "id")
	entry := m.l2Store.GetEntry(id)
	if entry == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "L2 session not found"})
		return
	}

	workDir := expandTilde(entry.WorkDir)
	if workDir == "" {
		m.writeJSON(w, http.StatusOK, ChangesResponse{Changes: []FileChange{}, Plans: entry.Plans})
		return
	}

	// Check if workDir exists.
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		m.writeJSON(w, http.StatusOK, ChangesResponse{Changes: []FileChange{}, Plans: entry.Plans})
		return
	}

	// For git repos: always use git diff.
	// - If we captured a base_ref at session start → git diff <base_ref> (session-scoped)
	// - If no base_ref (session not activated, old session, etc.) → git diff HEAD (all uncommitted)
	isGit := isGitRepo(workDir)
	if isGit {
		ref := entry.GitBaseRef
		if ref == "" {
			ref = "HEAD"
		}
		changes, err := computeGitSummary(workDir, ref)
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		resp := summarizeChanges(changes)
		resp.IsGitRepo = true
		resp.BaseRef = ref
		resp.Plans = entry.Plans
		m.writeJSON(w, http.StatusOK, resp)
		return
	}

	// Non-git: compare against snapshot.
	snapshot := session.LoadBaseline(m.l2Store.WorkDir(), id)
	if snapshot == nil {
		m.writeJSON(w, http.StatusOK, ChangesResponse{Changes: []FileChange{}, Plans: entry.Plans})
		return
	}

	changes := computeSnapshotSummary(workDir, snapshot)
	resp := summarizeChanges(changes)
	resp.IsGitRepo = false
	resp.Plans = entry.Plans
	m.writeJSON(w, http.StatusOK, resp)
}

// handleGetSessionChangeDiff returns diff content for one path from the current
// session change set. Exact membership validation prevents arbitrary file reads.
func (m *Mux) handleGetSessionChangeDiff(w http.ResponseWriter, r *http.Request) {
	if m.l2Store == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
		return
	}
	entry := m.l2Store.GetEntry(chi.URLParam(r, "id"))
	if entry == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "L2 session not found"})
		return
	}
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	expectedRevision := r.URL.Query().Get("expected_revision")
	if expectedRevision == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected_revision is required"})
		return
	}
	if !validChangePath(requestedPath) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	workDir := expandTilde(entry.WorkDir)
	if info, err := os.Stat(workDir); workDir == "" || err != nil || !info.IsDir() {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid work directory"})
		return
	}

	if isGitRepo(workDir) {
		ref := entry.GitBaseRef
		if ref == "" {
			ref = "HEAD"
		}
		change, err := loadGitDiffForPath(workDir, ref, requestedPath, expectedRevision, computeGitHunks)
		if errors.Is(err, errChangeRevisionConflict) {
			m.writeJSON(w, http.StatusConflict, map[string]string{"error": "change revision is stale", "code": "stale_revision"})
			return
		}
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if change == nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is not in the current change set"})
			return
		}
		m.writeJSON(w, http.StatusOK, change)
		return
	}

	snapshot := session.LoadBaseline(m.l2Store.WorkDir(), chi.URLParam(r, "id"))
	if snapshot == nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session baseline not available"})
		return
	}
	change, ok := computeSnapshotDiffForPath(workDir, snapshot, requestedPath)
	if !ok {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is not in the current change set"})
		return
	}
	if change.Revision != expectedRevision {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": "change revision is stale", "code": "stale_revision"})
		return
	}
	m.writeJSON(w, http.StatusOK, change)
}

// computeGitSummary collects metadata for all changed files with a fixed number
// of git commands. Diff hunks are loaded separately by path.
func computeGitSummary(workDir, baseRef string) ([]FileChange, error) {
	statusCmd := exec.Command("git", "-C", workDir, "diff", "--name-status", "--no-renames", baseRef)
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w", err)
	}

	numstatCmd := exec.Command("git", "-C", workDir, "diff", "--numstat", "--no-renames", baseRef)
	numstatOutput, err := numstatCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat: %w", err)
	}
	type counts struct {
		additions int
		deletions int
		binary    bool
	}
	stats := make(map[string]counts)
	for _, line := range strings.Split(strings.TrimSpace(string(numstatOutput)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "-" && parts[1] == "-" {
			stats[parts[2]] = counts{binary: true}
			continue
		}
		additions, _ := strconv.Atoi(parts[0])
		deletions, _ := strconv.Atoi(parts[1])
		stats[parts[2]] = counts{additions: additions, deletions: deletions}
	}

	var changes []FileChange
	for _, line := range strings.Split(strings.TrimSpace(string(statusOutput)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := mapStatusCode(parts[0])
		if status == "" {
			continue
		}
		filePath := parts[1]
		stat := stats[filePath]
		change := FileChange{
			Path:      filePath,
			Status:    status,
			Additions: stat.additions,
			Deletions: stat.deletions,
			Binary:    stat.binary,
		}
		if stat.binary {
			if info, statErr := os.Stat(filepath.Join(workDir, filepath.FromSlash(filePath))); statErr == nil {
				change.SizeBytes = info.Size()
			}
		}
		change.Revision = computeChangeRevision(workDir, baseRef, change)
		changes = append(changes, change)
	}
	return changes, nil
}

// computeGitChangeSummary inspects only requestedPath. It deliberately avoids
// scanning or hashing the rest of the current change set.
func computeGitChangeSummary(workDir, baseRef, requestedPath string) (*FileChange, error) {
	if !validChangePath(requestedPath) {
		return nil, nil
	}
	statusOutput, err := exec.Command(
		"git", "-C", workDir, "diff", "--name-status", "--no-renames", baseRef, "--", requestedPath,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w", err)
	}
	status := ""
	for _, line := range strings.Split(strings.TrimSpace(string(statusOutput)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] == requestedPath {
			status = mapStatusCode(parts[0])
			break
		}
	}
	if status == "" {
		return nil, nil
	}

	numstatOutput, err := exec.Command(
		"git", "-C", workDir, "diff", "--numstat", "--no-renames", baseRef, "--", requestedPath,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat: %w", err)
	}
	change := &FileChange{Path: requestedPath, Status: status}
	for _, line := range strings.Split(strings.TrimSpace(string(numstatOutput)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 || parts[2] != requestedPath {
			continue
		}
		if parts[0] == "-" && parts[1] == "-" {
			change.Binary = true
			break
		}
		change.Additions, _ = strconv.Atoi(parts[0])
		change.Deletions, _ = strconv.Atoi(parts[1])
		break
	}
	if change.Binary {
		if info, statErr := os.Lstat(filepath.Join(workDir, filepath.FromSlash(requestedPath))); statErr == nil {
			change.SizeBytes = info.Size()
		}
	}
	change.Revision = computeChangeRevision(workDir, baseRef, *change)
	return change, nil
}

func loadGitDiffForPath(
	workDir, baseRef, requestedPath, expectedRevision string,
	loadHunks func(string, string, string) ([]DiffHunk, error),
) (*FileChange, error) {
	before, err := computeGitChangeSummary(workDir, baseRef, requestedPath)
	if err != nil || before == nil {
		return before, err
	}
	if before.Revision != expectedRevision {
		return nil, errChangeRevisionConflict
	}
	var hunks []DiffHunk
	if !before.Binary {
		hunks, err = loadHunks(workDir, baseRef, requestedPath)
		if err != nil {
			return nil, err
		}
	}
	after, err := computeGitChangeSummary(workDir, baseRef, requestedPath)
	if err != nil {
		return nil, err
	}
	if after == nil || after.Revision != expectedRevision {
		return nil, errChangeRevisionConflict
	}
	after.Hunks = hunks
	return after, nil
}

func computeChangeRevision(workDir, baseRef string, change FileChange) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(
		hash,
		"%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%t\x00",
		baseRef, change.Status, change.Path, change.OldPath,
		change.Additions, change.Deletions, change.Binary,
	)
	fullPath := filepath.Join(workDir, filepath.FromSlash(change.Path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		_, _ = io.WriteString(hash, "missing")
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	_, _ = fmt.Fprintf(hash, "mode=%d\x00size=%d\x00mtime=%d\x00", info.Mode(), info.Size(), info.ModTime().UnixNano())
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(fullPath)
		if readErr == nil {
			_, _ = io.WriteString(hash, "symlink\x00"+target)
		}
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	if !info.Mode().IsRegular() || info.Size() > maxChangeRevisionBytes {
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	file, openErr := os.OpenFile(fullPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if openErr == nil {
		openedInfo, statErr := file.Stat()
		if statErr == nil && openedInfo.Mode().IsRegular() && os.SameFile(info, openedInfo) {
			_, _ = io.CopyN(hash, file, maxChangeRevisionBytes+1)
		}
		_ = file.Close()
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// ─── Git Diff ────────────────────────────────────────────────────────────────

// isGitRepo checks if workDir is inside a git working tree.
func isGitRepo(workDir string) bool {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "--is-inside-work-tree")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// computeGitHunks parses the unified diff output for a single file into DiffHunks.
func computeGitHunks(workDir, baseRef, filePath string) ([]DiffHunk, error) {
	cmd := exec.Command("git", "-C", workDir, "diff", baseRef, "--", filePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	return parseUnifiedDiff(string(output)), nil
}

// parseUnifiedDiff parses `git diff` output into DiffHunks.
func parseUnifiedDiff(diff string) []DiffHunk {
	lines := strings.Split(diff, "\n")
	var hunks []DiffHunk
	var currentHunk *DiffHunk
	var currentLines []DiffLine

	finishHunk := func() {
		if currentHunk != nil && len(currentLines) > 0 {
			currentHunk.Lines = currentLines
			hunks = append(hunks, *currentHunk)
		}
		currentHunk = nil
		currentLines = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			finishHunk()
			hunk := parseHunkHeader(line)
			currentHunk = &hunk
			currentLines = nil
			continue
		}
		if currentHunk == nil {
			continue // skip diff header lines (diff, index, ---, +++, etc.)
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case '+':
			currentLines = append(currentLines, DiffLine{Type: "add", Content: line[1:]})
		case '-':
			currentLines = append(currentLines, DiffLine{Type: "del", Content: line[1:]})
		case ' ':
			currentLines = append(currentLines, DiffLine{Type: "ctx", Content: line[1:]})
		default:
			// Skip "\ No newline at end of file" etc.
		}
	}
	finishHunk()
	return hunks
}

// parseHunkHeader parses "@@ -old_start,old_lines +new_start,new_lines @@".
func parseHunkHeader(line string) DiffHunk {
	// Format: @@ -a,b +c,d @@
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return DiffHunk{}
	}
	oldPart := strings.TrimPrefix(parts[1], "-")
	newPart := strings.TrimPrefix(parts[2], "+")

	oldStart, oldLines := parseRange(oldPart)
	newStart, newLines := parseRange(newPart)

	return DiffHunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
	}
}

// parseRange parses "start,count" or "start" (count defaults to 1).
func parseRange(s string) (int, int) {
	parts := strings.SplitN(s, ",", 2)
	start, _ := strconv.Atoi(parts[0])
	count := 1
	if len(parts) > 1 {
		count, _ = strconv.Atoi(parts[1])
	}
	return start, count
}

// mapStatusCode converts git status code to our status string.
func mapStatusCode(code string) string {
	switch code[0] {
	case 'A':
		return "added"
	case 'M':
		return "modified"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	default:
		return ""
	}
}

// ─── Non-Git Snapshot Diff ──────────────────────────────────────────────────

func computeSnapshotSummary(workDir string, snapshot map[string]string) []FileChange {
	current := make(map[string]string)
	currentFiles := make(map[string]bool)
	walkAndHash(workDir, "", current, currentFiles)

	var changes []FileChange
	for path, hash := range current {
		oldHash, existed := snapshot[path]
		if existed && oldHash == hash {
			continue
		}
		status := "added"
		if existed {
			status = "modified"
		}
		change := FileChange{Path: path, Status: status, Revision: hash}
		if content, _, binary, size, ok := readSnapshotTarget(filepath.Join(workDir, filepath.FromSlash(path))); ok {
			change.Binary = binary
			change.SizeBytes = size
			if !binary {
				change.Additions = len(strings.Split(string(content), "\n"))
			}
		}
		changes = append(changes, change)
	}
	for path, oldHash := range snapshot {
		if !currentFiles[path] {
			changes = append(changes, FileChange{Path: path, Status: "deleted", Revision: oldHash})
		}
	}
	return changes
}

func computeSnapshotDiffForPath(
	workDir string,
	snapshot map[string]string,
	requestedPath string,
) (FileChange, bool) {
	if !validChangePath(requestedPath) {
		return FileChange{}, false
	}
	oldHash, existed := snapshot[requestedPath]
	content, revision, binary, size, exists := readSnapshotTarget(
		filepath.Join(workDir, filepath.FromSlash(requestedPath)),
	)
	if !exists {
		if existed {
			return FileChange{Path: requestedPath, Status: "deleted", Revision: oldHash}, true
		}
		return FileChange{}, false
	}
	if existed && oldHash == revision {
		return FileChange{}, false
	}
	status := "added"
	if existed {
		status = "modified"
	}
	change := FileChange{
		Path: requestedPath, Status: status, Revision: revision, Binary: binary, SizeBytes: size,
	}
	if binary {
		return change, true
	}
	newLines := strings.Split(string(content), "\n")
	lines := make([]DiffLine, 0, len(newLines))
	for i, line := range newLines {
		lines = append(lines, DiffLine{Type: "add", Content: line, NewNum: i + 1})
	}
	change.Additions = len(newLines)
	if len(lines) > 0 {
		change.Hunks = []DiffHunk{{NewStart: 1, NewLines: len(newLines), Lines: lines}}
	}
	return change, true
}

// readSnapshotTarget returns one coherent bounded read. The returned revision
// is derived from the same bytes used to build the diff.
func readSnapshotTarget(fullPath string) ([]byte, string, bool, int64, bool) {
	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, "", false, 0, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(fullPath)
		if readErr != nil {
			return nil, "", false, 0, false
		}
		sum := sha256.Sum256([]byte("symlink\x00" + target))
		return nil, fmt.Sprintf("%x", sum[:]), true, info.Size(), true
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxChangeRevisionBytes {
		return nil, "", false, 0, false
	}
	file, err := os.OpenFile(fullPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", false, 0, false
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, "", false, 0, false
	}
	content, err := io.ReadAll(io.LimitReader(file, maxChangeRevisionBytes+1))
	if err != nil || int64(len(content)) > maxChangeRevisionBytes {
		return nil, "", false, 0, false
	}
	afterInfo, err := file.Stat()
	if err != nil || afterInfo.Size() != openedInfo.Size() ||
		afterInfo.ModTime() != openedInfo.ModTime() || afterInfo.Mode() != openedInfo.Mode() {
		return nil, "", false, 0, false
	}
	sum := sha256.Sum256(content)
	return content, fmt.Sprintf("%x", sum[:]), false, int64(len(content)), true
}

// walkAndHash recursively walks the directory and hashes text files.
func walkAndHash(baseDir, relDir string, hashes map[string]string, files map[string]bool) {
	fullDir := baseDir
	if relDir != "" {
		fullDir = filepath.Join(baseDir, relDir)
	}
	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		relPath := filepath.Join(relDir, entry.Name())
		if entry.IsDir() {
			name := entry.Name()
			if isExcludedDir(name) || strings.HasPrefix(name, ".") {
				continue
			}
			walkAndHash(baseDir, relPath, hashes, files)
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if isExcludedExt(ext) {
			continue
		}
		fullPath := filepath.Join(baseDir, relPath)
		_, hash, _, _, ok := readSnapshotTarget(fullPath)
		if !ok {
			continue
		}
		slashPath := filepath.ToSlash(relPath)
		hashes[slashPath] = hash
		files[slashPath] = true
	}
}

func validChangePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func isExcludedDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "out", ".next",
		".svelte-kit", "vendor", "__pycache__", ".cache", "target":
		return true
	default:
		return false
	}
}

func isExcludedExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp", ".svg",
		".mp4", ".mp3", ".wav", ".avi", ".mov",
		".zip", ".gz", ".tar", ".rar", ".7z",
		".pdf", ".doc", ".docx", ".xls", ".xlsx",
		".exe", ".dll", ".so", ".dylib", ".o", ".a", ".wasm", ".class", ".jar",
		".ttf", ".otf", ".woff", ".woff2",
		".sqlite", ".db":
		return true
	default:
		return false
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// summarizeChanges computes totals and returns a ChangesResponse.
func summarizeChanges(changes []FileChange) ChangesResponse {
	summaries := make([]FileChange, len(changes))
	copy(summaries, changes)
	resp := ChangesResponse{Changes: summaries}
	for i := range resp.Changes {
		c := &resp.Changes[i]
		resp.TotalAdditions += c.Additions
		resp.TotalDeletions += c.Deletions
		c.Hunks = nil
	}
	return resp
}
