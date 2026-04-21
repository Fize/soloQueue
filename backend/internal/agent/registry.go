package agent

import "sync"

// Registry 是 agent ID → Agent 的并发安全映射
//
// 本 phase 不包含生命周期管理（没有 Stop/Shutdown），因为 agent 无后台资源。
// Unregister 只是从映射里移除条目。
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
}

// NewRegistry 构造空 registry
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]*Agent)}
}

// Register 添加 agent；ID 已存在返回 ErrAgentAlreadyExists
// agent 为 nil 返回 ErrAgentNil；Def.ID 为空返回 ErrEmptyID
func (r *Registry) Register(a *Agent) error {
	if a == nil {
		return ErrAgentNil
	}
	id := a.Def.ID
	if id == "" {
		return ErrEmptyID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[id]; exists {
		return ErrAgentAlreadyExists
	}
	r.agents[id] = a
	return nil
}

// Unregister 从 registry 移除 ID；返回 true 表示确实存在并被移除
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[id]; !exists {
		return false
	}
	delete(r.agents, id)
	return true
}

// Get 查找 agent；不存在返回 (nil, false)
func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// List 返回当前所有 agent 的快照切片
//
// 返回的切片与内部 map 独立，修改切片不影响 registry；
// 切片元素仍是 *Agent 指针，修改 Agent 字段会反映到内部（通常不应发生）。
func (r *Registry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}

// Len 当前 registry 中 agent 的数量
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}
