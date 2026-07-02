package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Baseline captures the state of a working directory at session start.
//
// For git repos: stores the HEAD commit hash (GitBaseRef). git diff <ref>
// at display time provides full line-level diffs.
//
// For non-git repos: stores a path→content-hash snapshot of all text files.
// At display time, current files are re-hashed and compared; changed files
// are line-diffed against the snapshot's original content.
type Baseline struct {
	GitBaseRef string            `json:"git_base_ref,omitempty"` // non-empty when workDir is a git repo
	Snapshot   map[string]string `json:"snapshot,omitempty"`     // path→sha256, non-git only
}

// excludedDirs are directories skipped during non-git snapshot traversal.
var excludedDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"dist":          true,
	"build":         true,
	"out":           true,
	".next":         true,
	".svelte-kit":   true,
	"vendor":        true,
	"__pycache__":   true,
	".cache":        true,
	"target":        true,
}

// excludedExts are file extensions skipped during snapshot (binaries, etc).
var excludedExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true, ".svg": true,
	".mp4": true, ".mp3": true, ".wav": true, ".avi": true, ".mov": true,
	".zip": true, ".gz": true, ".tar": true, ".rar": true, ".7z": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".o": true,
	".a": true, ".wasm": true, ".class": true, ".jar": true,
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
	".sqlite": true, ".db": true,
}

// CaptureBaseline captures the working directory state at session start.
// If workDir is a git repo, returns GitBaseRef (HEAD commit hash).
// Otherwise, returns a Snapshot of all text files (path→sha256).
// Returns an empty Baseline if workDir is empty or doesn't exist.
func CaptureBaseline(workDir string) Baseline {
	if workDir == "" {
		return Baseline{}
	}
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		return Baseline{}
	}

	// Try git first.
	if ref := captureGitBaseRef(workDir); ref != "" {
		return Baseline{GitBaseRef: ref}
	}

	// Non-git: snapshot all text files.
	return Baseline{Snapshot: captureFileSnapshot(workDir)}
}

// captureGitBaseRef returns the HEAD commit hash, or "" if not a git repo.
func captureGitBaseRef(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// captureFileSnapshot walks workDir and returns path→sha256 for all text files.
func captureFileSnapshot(workDir string) map[string]string {
	snapshot := make(map[string]string)
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			name := d.Name()
			if name != filepath.Base(workDir) && excludedDirs[name] {
				return filepath.SkipDir
			}
			// Skip hidden directories (except the root itself)
			if strings.HasPrefix(name, ".") && name != filepath.Base(workDir) {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip excluded extensions
		ext := strings.ToLower(filepath.Ext(path))
		if excludedExts[ext] {
			return nil
		}
		// Skip large files (> 1MB) and empty files
		info, err := d.Info()
		if err != nil || info.Size() == 0 || info.Size() > 1<<20 {
			return nil
		}
		relPath, err := filepath.Rel(workDir, path)
		if err != nil {
			return nil
		}
		hash, err := HashFile(path)
		if err != nil {
			return nil
		}
		snapshot[filepath.ToSlash(relPath)] = hash
		return nil
	})
	return snapshot
}

// HashFile returns the SHA-256 hex digest of a file's content.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ─── Line Diff (LCS-based, zero-dependency) ────────────────────────────────

// DiffOp represents a single line-level diff operation.
type DiffOp struct {
	Type    string // "add", "del", "ctx"
	Line    string
	OldLine int // 1-indexed, 0 for added lines
	NewLine int // 1-indexed, 0 for deleted lines
}

// ComputeLineDiff produces a line-level diff between old and new text.
// Uses the classic LCS algorithm. Output is a sequence of DiffOps.
func ComputeLineDiff(oldText, newText string) []DiffOp {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	// LCS table
	m, n := len(oldLines), len(newLines)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []DiffOp
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			ops = append(ops, DiffOp{Type: "ctx", Line: oldLines[i], OldLine: i + 1, NewLine: j + 1})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			ops = append(ops, DiffOp{Type: "del", Line: oldLines[i], OldLine: i + 1, NewLine: 0})
			i++
		} else {
			ops = append(ops, DiffOp{Type: "add", Line: newLines[j], OldLine: 0, NewLine: j + 1})
			j++
		}
	}
	for i < m {
		ops = append(ops, DiffOp{Type: "del", Line: oldLines[i], OldLine: i + 1, NewLine: 0})
		i++
	}
	for j < n {
		ops = append(ops, DiffOp{Type: "add", Line: newLines[j], OldLine: 0, NewLine: j + 1})
		j++
	}
	return ops
}

// ─── Baseline file I/O ──────────────────────────────────────────────────────

// baselineFilePath returns the path to the snapshot file for a session.
// The snapshot (non-git only) is stored separately from meta to avoid
// bloating the small meta JSON.
func baselineFilePath(workDir, sessionID string) string {
	return filepath.Join(workDir, "logs", "timelines", "l2-"+sessionID, "baseline")
}

// SaveBaseline persists the baseline to disk.
// GitBaseRef goes into the meta file (small). Snapshot goes into a
// separate baseline file (can be large for big non-git projects).
func SaveBaseline(workDir, sessionID string, b Baseline) {
	if b.Snapshot != nil {
		// Write snapshot to a separate file
		path := baselineFilePath(workDir, sessionID)
		var buf strings.Builder
		// Simple format: "hash\tpath\n" per line, sorted by path for determinism.
		keys := make([]string, 0, len(b.Snapshot))
		for k := range b.Snapshot {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			buf.WriteString(b.Snapshot[k])
			buf.WriteByte('\t')
			buf.WriteString(k)
			buf.WriteByte('\n')
		}
		_ = os.WriteFile(path, []byte(buf.String()), 0644)
	}
}

// LoadBaseline reads the snapshot baseline from disk (non-git only).
func LoadBaseline(workDir, sessionID string) map[string]string {
	path := baselineFilePath(workDir, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	snapshot := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.IndexByte(line, '\t')
		if idx <= 0 {
			continue
		}
		hash := line[:idx]
		filePath := line[idx+1:]
		snapshot[filePath] = hash
	}
	return snapshot
}
