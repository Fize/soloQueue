package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

	for _, gf := range m.groups {
		for _, ws := range gf.Frontmatter.Workspaces {
			p := expandTilde(ws.Path)
			p = filepath.Clean(p)
			if p != "" && p != "@default" {
				roots = append(roots, p)
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
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
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

// FileRoot describes a browsable root directory for the file browser.
type FileRoot struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Group string `json:"group"`
}

// handleGetFileRoots returns all configured browse roots:
// global Plan directory + each group's workspace directories.
func (m *Mux) handleGetFileRoots(w http.ResponseWriter, r *http.Request) {
	planDir := filepath.Join(m.workDir, "plan")
	roots := []FileRoot{
		{Label: "Plan", Path: planDir, Group: ""},
	}

	for key, gf := range m.groups {
		name := gf.Frontmatter.Name
		if name == "" {
			name = key
		}
		for _, ws := range gf.Frontmatter.Workspaces {
			p := expandTilde(ws.Path)
			p = filepath.Clean(p)
			if p == "" || p == "@default" {
				continue
			}
			wsName := ws.Name
			if wsName == "" {
				wsName = filepath.Base(p)
			}
			roots = append(roots, FileRoot{Label: wsName, Path: p, Group: name})
		}
	}

	m.writeJSON(w, http.StatusOK, roots)
}

func (m *Mux) handleGetFileInfo(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	absPath, err := m.validatePath(raw)
	if err != nil {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "path not in allowed directories"})
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	info := FileInfo{
		Name:    filepath.Base(absPath),
		Path:    absPath,
		Size:    fi.Size(),
		IsDir:   fi.IsDir(),
		Ext:     ext,
		ModTime: fi.ModTime().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}
