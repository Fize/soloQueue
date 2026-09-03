package skill

import (
	"os"
	"path/filepath"
	"strings"
)

// SkillFileEntry represents a single file or directory inside a skill.
type SkillFileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "file" or "directory"
	Size *int64 `json:"size,omitempty"`
}

// ListSkillFiles recursively lists files inside an installed skill directory.
func ListSkillFiles(skillDir string) ([]SkillFileEntry, error) {
	var out []SkillFileEntry
	seen := make(map[string]bool)
	const maxEntries = 500
	const maxDepth = 6

	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if depth > maxDepth || len(out) >= maxEntries {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(out) >= maxEntries || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			abs := filepath.Join(dir, entry.Name())
			rel, err := filepath.Rel(skillDir, abs)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true

			if entry.IsDir() {
				out = append(out, SkillFileEntry{Path: rel, Kind: "directory"})
				if err := walk(abs, depth+1); err != nil {
					return err
				}
				continue
			}

			var size *int64
			if info, err := entry.Info(); err == nil {
				sz := info.Size()
				size = &sz
			}
			out = append(out, SkillFileEntry{Path: rel, Kind: "file", Size: size})
		}
		return nil
	}

	return out, walk(skillDir, 0)
}
