package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FileInfo holds metadata for a single file or directory.
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	Ext     string `json:"ext"`
	ModTime string `json:"modTime"`
}

// textExtensions maps lowercase file extensions that should be served as text/plain.
var textExtensions = map[string]bool{
	".md": true, ".markdown": true,
	".go": true, ".mod": true, ".sum": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".py": true, ".pyi": true, ".pyx": true,
	".rs": true, ".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true, ".hxx": true,
	".java": true, ".kt": true, ".kts": true, ".scala": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true,
	".css": true, ".scss": true, ".less": true,
	".html": true, ".htm": true, ".xml": true, ".svg": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".sql": true, ".psql": true,
	".txt": true, ".log": true,
	".Makefile": true, ".dockerfile": true, ".dockerignore": true, ".gitignore": true,
	".env": true, ".envrc": true,
	".proto": true, ".graphql": true, ".gql": true,
	".lua": true, ".rb": true, ".php": true, ".swift": true, ".r": true, ".dart": true,
	".tf": true, ".hcl": true,
	".vue": true, ".svelte": true,
}

// expandTilde replaces a leading ~ or ~user with the home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// allowedRoots returns the set of absolute root directories that files may be served from.
func (m *Mux) allowedRoots() []string {
	var roots []string

	planDir := filepath.Join(m.workDir, "plan")
	roots = append(roots, planDir)

	workspaceDir := filepath.Join(m.workDir, "workspace")
	roots = append(roots, workspaceDir)

	// Generated images from ImageGenerate and ImageEdit tools.
	imagesDir := filepath.Join(m.workDir, "images")
	roots = append(roots, imagesDir)
	artifactsDir := filepath.Join(m.workDir, "artifacts")
	roots = append(roots, artifactsDir)

	if m.teamstore != nil {
		projects, err := m.teamstore.ListProjects(context.Background())
		if err == nil {
			for _, p := range projects {
				cleanPath := filepath.Clean(expandTilde(p.Path))
				if cleanPath != "" {
					roots = append(roots, cleanPath)
				}
			}
		}
	}

	return roots
}

// validatePath resolves and validates that the requested path is under an allowed root.
// Returns the resolved absolute path or an error.
func (m *Mux) validatePath(raw string) (string, error) {
	if raw == "" {
		return "", os.ErrNotExist
	}

	expanded := expandTilde(raw)
	cleaned := filepath.Clean(expanded)

	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}

	for _, root := range m.allowedRoots() {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolvedRoot = root
		}
		if resolved == resolvedRoot || strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
			return resolved, nil
		}
	}

	return "", os.ErrPermission
}

// handleGetFileContent serves a file's content from the plan directory or team workspaces.
func (m *Mux) handleGetFileContent(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	absPath, err := m.validatePath(raw)
	if err != nil {
		if os.IsNotExist(err) {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "path not in allowed directories"})
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil || fi.IsDir() {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if textExtensions[ext] {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}

	http.ServeFile(w, r, absPath)
}

// handleListFiles lists files and directories under a given directory.
func (m *Mux) handleListFiles(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("dir")
	if raw == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing dir parameter"})
		return
	}

	absDir, err := m.validatePath(raw)
	if err != nil {
		if os.IsNotExist(err) {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "directory not found"})
			return
		}
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "path not in allowed directories"})
		return
	}

	fi, err := os.Stat(absDir)
	if err != nil || !fi.IsDir() {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "directory not found"})
		return
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		entryPath := filepath.Join(absDir, name)
		ext := strings.ToLower(filepath.Ext(name))
		files = append(files, FileInfo{
			Name:    name,
			Path:    entryPath,
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			Ext:     ext,
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	m.writeJSON(w, http.StatusOK, files)
}

var checkboxRegex = regexp.MustCompile(`(?m)(^|[\r\n])(\s*(?:[*+-]|\d+\.)\s+)\[([ x/])\]`)

// handleToggleCheckbox toggles the sequential N-th checkbox in a markdown file.
func (m *Mux) handleToggleCheckbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Index int    `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	absPath, err := m.validatePath(req.Path)
	if err != nil {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}

	content := string(data)
	matches := checkboxRegex.FindAllStringSubmatchIndex(content, -1)
	if req.Index < 0 || req.Index >= len(matches) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "checkbox index out of range"})
		return
	}

	match := matches[req.Index]
	if len(match) < 8 {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal parsing error"})
		return
	}

	charIdx := match[6]
	oldChar := content[charIdx]
	var newChar rune
	if oldChar == ' ' || oldChar == '/' {
		newChar = 'x'
	} else {
		newChar = ' '
	}

	newContent := content[:charIdx] + string(newChar) + content[charIdx+1:]
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write file"})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}


