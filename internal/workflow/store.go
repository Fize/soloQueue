package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store manages workflow YAML files on disk.
// Files are stored as <Dir>/<name>.yaml.
// Security: rejects path traversal, symlinks outside Dir, and name/filename mismatches.
type Store struct {
	Dir          string
	MaxFileBytes int64
}

// NewStore creates a Store for the given directory.
// maxFileBytes defaults to 1 MiB if <= 0.
func NewStore(dir string, maxFileBytes int64) *Store {
	if maxFileBytes <= 0 {
		maxFileBytes = 1 << 20 // 1 MiB
	}
	return &Store{Dir: dir, MaxFileBytes: maxFileBytes}
}

// EnsureDir creates the workflow directory if it doesn't exist.
func (s *Store) EnsureDir() error {
	return os.MkdirAll(s.Dir, 0755)
}

// ReadRaw returns the original YAML for a workflow after applying the same
// path and size checks as Load. Keeping this separate from Load lets the HTTP
// API round-trip comments and formatting unchanged.
func (s *Store) ReadRaw(name string) ([]byte, error) {
	if err := validateWorkflowName(name); err != nil {
		return nil, err
	}
	filePath := filepath.Join(s.Dir, name+".yaml")
	if err := s.validatePath(filePath); err != nil {
		return nil, err
	}
	fi, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("workflow: not found: %s", name)
		}
		return nil, fmt.Errorf("workflow: stat %s: %w", filePath, err)
	}
	if fi.Size() > s.MaxFileBytes {
		return nil, fmt.Errorf("workflow: file too large: %d bytes (max %d)", fi.Size(), s.MaxFileBytes)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("workflow: read %s: %w", filePath, err)
	}
	return data, nil
}

// Save validates and atomically replaces a workflow definition. The filename
// and the YAML name must match so callers cannot create ambiguous definitions.
func (s *Store) Save(name string, data []byte) (*WorkflowMeta, error) {
	if err := validateWorkflowName(name); err != nil {
		return nil, err
	}
	if int64(len(data)) > s.MaxFileBytes {
		return nil, fmt.Errorf("workflow: file too large: %d bytes (max %d)", len(data), s.MaxFileBytes)
	}
	pw, err := ParseWorkflow(data)
	if err != nil {
		return nil, err
	}
	if pw.Name != name {
		return nil, fmt.Errorf("workflow: name mismatch: YAML declares %q but request is %q", pw.Name, name)
	}
	if err := s.EnsureDir(); err != nil {
		return nil, fmt.Errorf("workflow: create store dir: %w", err)
	}
	filePath := filepath.Join(s.Dir, name+".yaml")
	if err := s.validatePath(filePath); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+"-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("workflow: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("workflow: write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("workflow: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("workflow: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("workflow: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return nil, fmt.Errorf("workflow: replace definition: %w", err)
	}
	return &WorkflowMeta{Name: pw.Name, Description: pw.Description, Version: pw.Version, Valid: true}, nil
}

// Delete removes one workflow definition. Active runs are intentionally not
// affected because they execute from the parsed definition captured at start.
func (s *Store) Delete(name string) error {
	if err := validateWorkflowName(name); err != nil {
		return err
	}
	filePath := filepath.Join(s.Dir, name+".yaml")
	if err := s.validatePath(filePath); err != nil {
		return err
	}
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workflow: not found: %s", name)
		}
		return fmt.Errorf("workflow: delete %s: %w", filePath, err)
	}
	return nil
}

// Load reads and validates a workflow by name.
// The name must match the filename and the YAML's "name" field.
func (s *Store) Load(name string) (*ParsedWorkflow, error) {
	data, err := s.ReadRaw(name)
	if err != nil {
		return nil, err
	}

	// Parse and validate
	pw, err := ParseWorkflow(data)
	if err != nil {
		return nil, err
	}

	// Verify name consistency: YAML name == requested name
	if pw.Name != name {
		return nil, fmt.Errorf("workflow: name mismatch: YAML declares %q but file is %q.yaml", pw.Name, name)
	}

	return pw, nil
}

// List returns metadata for all .yaml files in the store directory.
// Invalid files are included with Valid=false and an Error string.
func (s *Store) List() ([]WorkflowMeta, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty store
		}
		return nil, fmt.Errorf("workflow: readdir %s: %w", s.Dir, err)
	}

	var result []WorkflowMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if !identifierPattern.MatchString(name) {
			result = append(result, WorkflowMeta{
				Name:  name,
				Valid: false,
				Error: fmt.Sprintf("invalid filename: must match %s", identifierPattern),
			})
			continue
		}

		meta := WorkflowMeta{Name: name}

		pw, err := s.Load(name)
		if err != nil {
			meta.Valid = false
			meta.Error = err.Error()
		} else {
			meta.Valid = true
			meta.Description = pw.Description
			meta.Version = pw.Version
		}
		result = append(result, meta)
	}
	return result, nil
}

// validatePath ensures the resolved file path is within the store directory
// and is not a symlink pointing outside the store.
func (s *Store) validatePath(filePath string) error {
	// Resolve to absolute path and clean
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("workflow: invalid path: %w", err)
	}

	absDir, err := filepath.Abs(s.Dir)
	if err != nil {
		return fmt.Errorf("workflow: invalid store dir: %w", err)
	}

	// Ensure path is within store directory
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return fmt.Errorf("workflow: path resolution error: %w", err)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("workflow: path traversal rejected: %s", filePath)
	}

	// Check for symlinks pointing outside (only if file exists)
	fi, err := os.Lstat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file doesn't exist yet — let the caller handle
		}
		return fmt.Errorf("workflow: lstat %s: %w", filePath, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(filePath)
		if err != nil {
			return fmt.Errorf("workflow: readlink %s: %w", filePath, err)
		}
		// Resolve symlink target relative to file's directory
		targetAbs := target
		if !filepath.IsAbs(target) {
			targetAbs = filepath.Join(filepath.Dir(absPath), target)
		}
		targetAbs, err = filepath.Abs(targetAbs)
		if err != nil {
			return fmt.Errorf("workflow: symlink resolution error: %w", err)
		}
		targetRel, err := filepath.Rel(absDir, targetAbs)
		if err != nil || strings.HasPrefix(targetRel, "..") || filepath.IsAbs(targetRel) {
			return fmt.Errorf("workflow: symlink outside store rejected: %s -> %s", filePath, target)
		}
	}

	return nil
}

// validateWorkflowName checks the name matches the identifier pattern
// and contains no path separators.
func validateWorkflowName(name string) error {
	if name == "" {
		return errors.New("workflow: name must not be empty")
	}
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("workflow: name %q must match %s", name, identifierPattern)
	}
	// Additional check: no path separators (already covered by regex, but belt-and-suspenders)
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("workflow: name %q contains path separator", name)
	}
	return nil
}
