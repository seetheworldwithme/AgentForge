package tool

import "sync"

// Registry 管理可用工具集合。V1 用全局 Default 实例。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// Default 是全局默认注册表，内置工具通过 init() 注册到这里。
var Default = NewRegistry()

// NewRegistry 创建空的 Registry。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个工具。同名工具会被覆盖（用于测试时替换）。
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get 按名称查找工具。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List 返回所有已注册工具的切片（顺序不保证）。
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}
