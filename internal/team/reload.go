// Package team provides passive auto-reload for agent/group files written by the LLM.
//
// When L1's file-writing tools (Write, Edit, MultiWrite, MultiEdit) write to the
// agents/ or groups/ directories, the wrapper automatically parses the file and
// instantiates the agent via the factory, registering delegate tools for new leaders.
//
// This package exists outside of tools/ to avoid a circular dependency:
// tools -> agent -> tools. team imports both tools and agent safely.
package team

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// AutoReloadConfig holds the dependencies needed for auto-reload behavior.
type AutoReloadConfig struct {
	AgentsDir    string
	GroupsDir    string
	AgentFactory agent.AgentFactory
	Logger       *logger.Logger

	// OnLeaderCreated is called after a leader agent is hot-instantiated.
	// The L1 session uses this to dynamically register a delegate_* tool.
	// tmpl is the full parsed template (not a reference — safe to retain).
	OnLeaderCreated func(ctx context.Context, name, group string, ag *agent.Agent, tmpl agent.AgentTemplate)

	// OnWorkerCreated is called after a non-leader agent is hot-instantiated.
	// The caller should add the worker to the appropriate supervisor via AdoptChild.
	// tmpl is the full parsed template (not a reference — safe to retain).
	OnWorkerCreated func(ctx context.Context, name, group string, ag *agent.Agent, tmpl agent.AgentTemplate)
}

// WrapWithAutoReload wraps a file-writing tool so that writes to the agents/
// or groups/ directories trigger automatic parsing and instantiation.
//
// Preserves Confirmable and AsyncTool interfaces from the inner tool.
func WrapWithAutoReload(inner tools.Tool, cfg *AutoReloadConfig) tools.Tool {
	return &reloadWrapper{inner: inner, cfg: cfg}
}

type reloadWrapper struct {
	inner tools.Tool
	cfg   *AutoReloadConfig
}

func (w *reloadWrapper) Name() string                { return w.inner.Name() }
func (w *reloadWrapper) Description() string         { return w.inner.Description() }
func (w *reloadWrapper) Parameters() json.RawMessage { return w.inner.Parameters() }

func (w *reloadWrapper) Execute(ctx context.Context, args string) (string, error) {
	result, err := w.inner.Execute(ctx, args)
	if err != nil {
		return result, err
	}

	paths := extractPaths(args)
	var notes []string
	for _, p := range paths {
		note := w.maybeReload(ctx, p)
		if note != "" {
			notes = append(notes, note)
		}
	}
	if len(notes) > 0 {
		result = result + "\n\n[auto] " + strings.Join(notes, "\n[auto] ")
	}
	return result, nil
}

// --- Confirmable passthrough ---

func (w *reloadWrapper) CheckConfirmation(args string) (bool, string) {
	if c, ok := w.inner.(tools.Confirmable); ok {
		return c.CheckConfirmation(args)
	}
	return false, ""
}

func (w *reloadWrapper) ConfirmationOptions(args string) []string {
	if c, ok := w.inner.(tools.Confirmable); ok {
		return c.ConfirmationOptions(args)
	}
	return nil
}

func (w *reloadWrapper) ConfirmArgs(originalArgs string, choice tools.ConfirmChoice) string {
	if c, ok := w.inner.(tools.Confirmable); ok {
		return c.ConfirmArgs(originalArgs, choice)
	}
	return originalArgs
}

func (w *reloadWrapper) SupportsSessionWhitelist() bool {
	if c, ok := w.inner.(tools.Confirmable); ok {
		return c.SupportsSessionWhitelist()
	}
	return false
}

// --- AsyncTool passthrough ---

func (w *reloadWrapper) ExecuteAsync(ctx context.Context, args string) (*tools.AsyncAction, error) {
	if a, ok := w.inner.(tools.AsyncTool); ok {
		return a.ExecuteAsync(ctx, args)
	}
	return nil, fmt.Errorf("tool %q does not support async execution", w.inner.Name())
}

func (w *reloadWrapper) IsAsync() bool {
	_, ok := w.inner.(tools.AsyncTool)
	return ok
}

// Compile-time checks.
var (
	_ tools.Tool        = (*reloadWrapper)(nil)
	_ tools.Confirmable = (*reloadWrapper)(nil)
	_ tools.AsyncTool   = (*reloadWrapper)(nil)
)

// --- Path extraction ---

// pathArgs is a generic struct for extracting the "path" field from tool args.
type pathArgs struct {
	Path  string     `json:"path"`
	Files []pathArgs `json:"files"`
	Edits []pathArgs `json:"edits"`
}

func extractPaths(args string) []string {
	var pa pathArgs
	if err := json.Unmarshal([]byte(args), &pa); err != nil {
		return nil
	}

	var paths []string
	if pa.Path != "" {
		paths = append(paths, pa.Path)
	}
	for _, f := range pa.Files {
		if f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	for _, e := range pa.Edits {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

// --- Reload logic ---

func (w *reloadWrapper) maybeReload(ctx context.Context, path string) string {
	absAgents, _ := filepath.Abs(w.cfg.AgentsDir)
	absGroups, _ := filepath.Abs(w.cfg.GroupsDir)

	absPath := path
	if !filepath.IsAbs(path) {
		absPath, _ = filepath.Abs(path)
	}

	if strings.HasPrefix(absPath, absAgents+string(filepath.Separator)) ||
		absPath == absAgents {
		return w.reloadAgent(ctx, path)
	}
	if strings.HasPrefix(absPath, absGroups+string(filepath.Separator)) ||
		absPath == absGroups {
		return w.reloadGroup(ctx, path)
	}
	return ""
}

func (w *reloadWrapper) reloadAgent(ctx context.Context, path string) string {
	af, err := prompt.ParseAgentFile(path)
	if err != nil {
		if w.cfg.Logger != nil {
			w.cfg.Logger.Info(logger.CatActor, "auto-reload: parse agent failed",
				"path", path, "err", err.Error())
		}
		return ""
	}

	fm := af.Frontmatter
	tmpl := agent.AgentTemplate{
		ID:           fm.Name,
		Name:         fm.Name,
		Description:  fm.Description,
		SystemPrompt: af.Body,
		ModelID:      fm.Model,
		IsLeader:     fm.IsLeader,
		Group:        fm.Group,
		MCPServers:   fm.MCPServers,
	}

	ag, _, err := w.cfg.AgentFactory.Create(ctx, tmpl)
	if err != nil {
		return fmt.Sprintf("Agent file '%s' written but instantiation failed: %v. Restart required.", fm.Name, err)
	}

	if fm.IsLeader && w.cfg.OnLeaderCreated != nil {
		w.cfg.OnLeaderCreated(ctx, fm.Name, fm.Group, ag, tmpl)
		return fmt.Sprintf("Leader '%s' (%s) created and activated. Use delegate_%s to assign tasks.", fm.Name, fm.Group, fm.Name)
	}

	if !fm.IsLeader && w.cfg.OnWorkerCreated != nil {
		w.cfg.OnWorkerCreated(ctx, fm.Name, fm.Group, ag, tmpl)
	}

	return fmt.Sprintf("Worker '%s' (%s) created and activated.", fm.Name, fm.Group)
}

func (w *reloadWrapper) reloadGroup(_ context.Context, path string) string {
	gf, err := prompt.ParseGroupFile(path)
	if err != nil {
		if w.cfg.Logger != nil {
			w.cfg.Logger.Info(logger.CatActor, "auto-reload: parse group failed",
				"path", path, "err", err.Error())
		}
		return ""
	}
	return fmt.Sprintf("Team '%s' registered. You can now create members for this team.", gf.Frontmatter.Name)
}
