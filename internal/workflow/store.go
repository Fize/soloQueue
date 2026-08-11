package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
	if err := s.writeAtomic(name, data); err != nil {
		return nil, err
	}
	return &WorkflowMeta{Name: pw.Name, Description: pw.Description, Version: pw.Version, Valid: true}, nil
}

// SaveDraft persists a syntactically valid but semantically incomplete workflow.
// Drafts can be edited and saved, but are rejected by Load and therefore cannot
// be executed until they pass strict workflow validation.
func (s *Store) SaveDraft(name string, data []byte) (*WorkflowMeta, error) {
	if int64(len(data)) > s.MaxFileBytes {
		return nil, fmt.Errorf("workflow: file too large: %d bytes (max %d)", len(data), s.MaxFileBytes)
	}
	meta, err := s.inspect(name, data)
	if err != nil {
		return nil, err
	}
	if meta.Valid {
		return nil, errors.New("workflow: draft is already valid; use Save")
	}
	if err := s.writeAtomic(name, data); err != nil {
		return nil, err
	}
	return meta, nil
}

// CreateDraft returns and persists the minimal definition used by the create
// flow. Users can fill in the graph from the editor before running it.
func (s *Store) CreateDraft(name string) (*WorkflowMeta, error) {
	if err := validateWorkflowName(name); err != nil {
		return nil, err
	}
	data := []byte(fmt.Sprintf("name: %s\nversion: \"1\"\ndescription: \"\"\nagents: {}\nentry: []\nnodes: []\n", name))
	return s.SaveDraft(name, data)
}

// Inspect returns metadata for valid workflows and editable drafts. It keeps
// strict execution validation in Load while allowing the editor to open a
// semantically incomplete definition.
func (s *Store) Inspect(name string) (*WorkflowMeta, error) {
	data, err := s.ReadRaw(name)
	if err != nil {
		return nil, err
	}
	return s.inspect(name, data)
}

func (s *Store) inspect(name string, data []byte) (*WorkflowMeta, error) {
	if err := validateWorkflowName(name); err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var def WorkflowDef
	if err := decoder.Decode(&def); err != nil {
		return nil, fmt.Errorf("workflow: YAML parse error: %w", err)
	}
	if err := validateWorkflowName(def.Name); err != nil {
		return nil, err
	}
	if def.Name != name {
		return nil, fmt.Errorf("workflow: name mismatch: YAML declares %q but request is %q", def.Name, name)
	}
	meta := &WorkflowMeta{Name: def.Name, Description: def.Description, Version: def.Version}
	if _, err := ParseWorkflow(data); err == nil {
		meta.Valid = true
		return meta, nil
	} else {
		meta.Draft = true
		meta.Error = err.Error()
	}
	return meta, nil
}

func (s *Store) writeAtomic(name string, data []byte) error {
	if err := s.EnsureDir(); err != nil {
		return fmt.Errorf("workflow: create store dir: %w", err)
	}
	filePath := filepath.Join(s.Dir, name+".yaml")
	if err := s.validatePath(filePath); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("workflow: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("workflow: write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("workflow: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("workflow: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workflow: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("workflow: replace definition: %w", err)
	}
	return nil
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

		inspected, err := s.Inspect(name)
		if err != nil {
			meta.Valid = false
			meta.Error = err.Error()
		} else {
			result = append(result, *inspected)
			continue
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
