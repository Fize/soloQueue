package tools

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// Tool-related errors.
var (
	// ErrToolNameEmpty indicates that Tool.Name() was empty at registration time.
	ErrToolNameEmpty = errors.New("tools: tool name is empty")
	// ErrToolAlreadyRegistered indicates that a tool with the same name was already registered.
	ErrToolAlreadyRegistered = errors.New("tools: tool already registered")
	// ErrToolNil indicates Register(nil) was called.
	ErrToolNil = errors.New("tools: tool is nil")
	// ErrToolNotFound indicates the tool name requested by the LLM was not found.
	ErrToolNotFound = errors.New("tools: tool not found")
)

// ToolRegistry is a concurrent-safe name → Tool mapping.
//
// Design principles:
//   - Register uses a write lock; Get / Specs / Len / Names use read locks.
//   - Specs() returns a fresh slice (not shared), and the tool count is typically <100 so copying is cheap.
//   - nil receiver is safe (SafeGet returns (nil,false) instead of panicking).
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry constructs an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register registers a tool.
//
// Errors:
//   - t == nil → ErrToolNil
//   - t.Name() == "" → ErrToolNameEmpty
//   - duplicate name → ErrToolAlreadyRegistered
func (r *ToolRegistry) Register(t Tool) error {
	if t == nil {
		return ErrToolNil
	}
	name := t.Name()
	if name == "" {
		return ErrToolNameEmpty
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, name)
	}
	r.tools[name] = t
	return nil
}

// Get finds a tool by name; missing tools return (nil, false).
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// SafeGet is a nil-receiver-friendly version of Get.
//
// Used by Agent.execTool; the agent may have no registered tools (a.caps == nil).
func (r *ToolRegistry) SafeGet(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	return r.Get(name)
}

// Specs returns a snapshot of all tool llm.ToolDef entries for LLMRequest.Tools.
//
// The order is by name in dictionary order (stable output helpful for log diffs and test assertions).
// An empty registry returns nil (not an empty slice).
func (r *ToolRegistry) Specs() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]llm.ToolDef, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		out = append(out, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDecl{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}

// Len returns the current number of tools.
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Names returns all tool names in dictionary order for logging/debugging.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// HasPrefix checks whether any tool name begins with prefix.
// Useful for quickly detecting whether an agent includes a class of tools (e.g. delegate_*, lsp__*).
// A nil receiver returns false.
func (r *ToolRegistry) HasPrefix(prefix string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Unregister is an internal removal method used by SkillRegistry rollback.
func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}
