package module

import "sync"

// Registry is a process-global index from module ID to implementation.
// cmd/scout/root.go calls RegisterAll at startup; lookups happen from
// the tui package's effect reducer and the cache orphan-purge step.
type Registry struct {
	mu       sync.RWMutex
	modules  map[string]Module
	aliasMap map[string]string // alias -> ID
	order    []string          // registration order, for deterministic iteration
}

func NewRegistry() *Registry {
	return &Registry{
		modules:  make(map[string]Module),
		aliasMap: make(map[string]string),
	}
}

func (r *Registry) Register(m Module) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := m.Manifest().ID
	if _, exists := r.modules[id]; exists {
		panic("module: duplicate ID " + id)
	}
	r.modules[id] = m
	r.order = append(r.order, id)
	for _, alias := range m.Manifest().Aliases {
		r.aliasMap[alias] = id
	}
}

func (r *Registry) Get(id string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules[id]
	return m, ok
}

func (r *Registry) Lookup(alias string) (Module, bool) {
	r.mu.RLock()
	id, ok := r.aliasMap[alias]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return r.Get(id)
}

func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

func (r *Registry) All() []Module {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Module, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.modules[id])
	}
	return out
}
