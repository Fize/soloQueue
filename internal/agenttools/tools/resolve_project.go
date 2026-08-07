package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

type resolveProjectTool struct {
	store    *store.Store
	executor *Executor
}

func newResolveProjectTool(cfg Config) *resolveProjectTool {
	return &resolveProjectTool{store: cfg.TeamStore, executor: cfg.Executor}
}

func (resolveProjectTool) Name() string { return "resolve_project" }

func (resolveProjectTool) Description() string {
	return "Resolve a project to its absolute filesystem path by searching across ID, name, or path. Case-insensitive fuzzy matching. Returns the exact match if unambiguous, or lists candidates when multiple projects match."
}

func (resolveProjectTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "query":{"type":"string","description":"Project ID, name, or path to resolve"}
  },
  "required":["query"]
}`)
}

type resolveProjectArgs struct {
	Query string `json:"query"`
}

type projectInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type resolveProjectResult struct {
	Project      *projectInfo  `json:"project,omitempty"`
	ResolvedPath string        `json:"resolved_path,omitempty"`
	Exists       bool          `json:"exists"`
	Ambiguous    bool          `json:"ambiguous,omitempty"`
	Candidates   []projectInfo `json:"candidates,omitempty"`
}

func (t *resolveProjectTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a resolveProjectArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}

	projects, err := t.store.ResolveProject(ctx, a.Query)
	if err != nil {
		return "", err
	}

	if len(projects) > 1 {
		candidates := make([]projectInfo, len(projects))
		for i, p := range projects {
			candidates[i] = projectInfo{ID: p.ID, Name: p.Name, Path: p.Path}
		}
		res := resolveProjectResult{
			Ambiguous:  true,
			Candidates: candidates,
		}
		b, _ := json.Marshal(res)
		return string(b), nil
	}

	p := projects[0]
	absPath, err := filepath.Abs(p.Path)
	if err != nil {
		absPath = p.Path
	}
	absPath = filepath.Clean(absPath)

	_, statErr := t.executor.Stat(ctx, absPath)
	exists := statErr == nil

	res := resolveProjectResult{
		Project:      &projectInfo{ID: p.ID, Name: p.Name, Path: p.Path},
		ResolvedPath: absPath,
		Exists:       exists,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*resolveProjectTool)(nil)
