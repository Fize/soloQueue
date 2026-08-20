package server

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

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
		changes, err := computeGitDiff(workDir, ref)
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

	changes := computeSnapshotDiff(workDir, snapshot)
	resp := summarizeChanges(changes)
	resp.IsGitRepo = false
	resp.Plans = entry.Plans
	m.writeJSON(w, http.StatusOK, resp)
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

// computeGitDiff runs git diff --name-status and git diff to produce FileChanges.
func computeGitDiff(workDir, baseRef string) ([]FileChange, error) {
	// Get file status list.
	statusCmd := exec.Command("git", "-C", workDir, "diff", "--name-status", "--no-renames", baseRef)
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w", err)
	}

	var changes []FileChange
	for _, line := range strings.Split(string(statusOutput), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		statusCode := parts[0]
		filePath := parts[1]

		status := mapStatusCode(statusCode)
		if status == "" {
			continue // skip unknown statuses
		}

		fc := FileChange{Path: filePath, Status: status}

		// Get per-file diff with stat.
		diffCmd := exec.Command("git", "-C", workDir, "diff", "--numstat", baseRef, "--", filePath)
		diffOutput, err := diffCmd.Output()
		if err == nil {
			fc.Additions, fc.Deletions = parseNumstat(string(diffOutput))
		}

		// Check if binary.
		if fc.Additions == 0 && fc.Deletions == 0 {
			binaryCmd := exec.Command("git", "-C", workDir, "diff", "--stat", baseRef, "--", filePath)
			binaryOutput, err := binaryCmd.Output()
			if err == nil && strings.Contains(string(binaryOutput), "Bin") {
				fc.Binary = true
				// Extract file size from "Bin 100 -> 200 bytes"
				fc.SizeBytes = extractBinarySize(string(binaryOutput))
			}
		}

		// Get full diff hunks for text files.
		if !fc.Binary {
			fc.Hunks = computeGitHunks(workDir, baseRef, filePath)
		}

		changes = append(changes, fc)
	}

	return changes, nil
}

// computeGitHunks parses the unified diff output for a single file into DiffHunks.
func computeGitHunks(workDir, baseRef, filePath string) []DiffHunk {
	cmd := exec.Command("git", "-C", workDir, "diff", baseRef, "--", filePath)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	return parseUnifiedDiff(string(output))
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

// parseNumstat parses "additions\tdeletions\tpath" output.
func parseNumstat(output string) (int, int) {
	output = strings.TrimSpace(output)
	if output == "" {
		return 0, 0
	}
	parts := strings.Split(output, "\t")
	if len(parts) < 2 {
		return 0, 0
	}
	add, _ := strconv.Atoi(parts[0])
	del, _ := strconv.Atoi(parts[1])
	return add, del
}

// extractBinarySize extracts the new file size from "Bin 100 -> 200 bytes".
func extractBinarySize(output string) int64 {
	// Find "->" and parse the number after it.
	idx := strings.Index(output, "->")
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(output[idx+2:])
	parts := strings.Fields(rest)
	if len(parts) < 1 {
		return 0
	}
	size, _ := strconv.ParseInt(parts[0], 10, 64)
	return size
}

// ─── Non-Git Snapshot Diff ──────────────────────────────────────────────────

// computeSnapshotDiff compares current filesystem state against the baseline snapshot.
func computeSnapshotDiff(workDir string, snapshot map[string]string) []FileChange {
	// Current file hashes.
	current := make(map[string]string)
	currentFiles := make(map[string]bool)
	walkAndHash(workDir, "", current, currentFiles)

	var changes []FileChange

	// Check for modified and added files.
	for path, hash := range current {
		oldHash, existed := snapshot[path]
		if !existed {
			// New file.
			fc := buildFileChangeFromSnapshot(workDir, path, "", hash, "added")
			changes = append(changes, fc)
		} else if oldHash != hash {
			// Modified file.
			fc := buildFileChangeFromSnapshot(workDir, path, oldHash, hash, "modified")
			changes = append(changes, fc)
		}
	}

	// Check for deleted files.
	for path := range snapshot {
		if !currentFiles[path] {
			changes = append(changes, FileChange{Path: path, Status: "deleted"})
		}
	}

	return changes
}

// buildFileChangeFromSnapshot creates a FileChange with line diff for non-git files.
func buildFileChangeFromSnapshot(workDir, relPath, oldHash, newHash, status string) FileChange {
	fc := FileChange{Path: relPath, Status: status}

	fullPath := filepath.Join(workDir, relPath)
	newContent, err := os.ReadFile(fullPath)
	if err != nil {
		fc.Binary = true
		return fc
	}

	// We don't have the old content stored (only the hash).
	// For non-git, we can only show the new content as additions.
	// To get a real diff, we'd need to store original content — but that's
	// a larger storage cost. For now, show current content as all-additions
	// for new files, and for modified files, show the full new content.
	//
	// This is a known limitation of the hash-only snapshot approach.
	newLines := strings.Split(string(newContent), "\n")
	var lines []DiffLine
	for i, line := range newLines {
		lines = append(lines, DiffLine{
			Type:    "add",
			Content: line,
			NewNum:  i + 1,
		})
	}
	if len(lines) > 0 {
		fc.Hunks = []DiffHunk{{
			NewStart: 1,
			NewLines: len(newLines),
			Lines:    lines,
		}}
		fc.Additions = len(newLines)
	}

	return fc
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
		info, err := entry.Info()
		if err != nil || info.Size() == 0 || info.Size() > 1<<20 {
			continue
		}
		fullPath := filepath.Join(baseDir, relPath)
		hash, err := session.HashFile(fullPath)
		if err != nil {
			continue
		}
		slashPath := filepath.ToSlash(relPath)
		hashes[slashPath] = hash
		files[slashPath] = true
	}
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
	resp := ChangesResponse{Changes: changes}
	for _, c := range changes {
		resp.TotalAdditions += c.Additions
		resp.TotalDeletions += c.Deletions
	}
	return resp
}
