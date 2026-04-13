package index

import (
	"sort"
	"sync"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// Memory is the in-RAM, read-mostly view of the cache that the TUI searches
// against. It is rebuilt on startup from a DB.LoadAll() call and mutated by
// SWR refresh commands via Upsert / DeleteMissing.
//
// The search layer iterates All() for fuzzy matching; this is intentionally
// simple (a linear scan over a slice) and fast enough for tens of thousands
// of resources. If that stops being true we'll swap in a smarter index —
// but YAGNI for Phase 1.
type Memory struct {
	mu sync.RWMutex
	// byTypeKey is the canonical map; All() derives a snapshot slice.
	byTypeKey map[typeKey]core.Resource
}

type typeKey struct {
	Type core.ResourceType
	Key  string
}

// NewMemory returns an empty in-memory index.
func NewMemory() *Memory {
	return &Memory{byTypeKey: make(map[typeKey]core.Resource)}
}

// Load replaces the entire index contents with rs. Used on startup.
func (m *Memory) Load(rs []core.Resource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byTypeKey = make(map[typeKey]core.Resource, len(rs))
	for _, r := range rs {
		m.byTypeKey[typeKey{r.Type, r.Key}] = r
	}
}

// Upsert inserts or updates resources. Safe for concurrent use.
func (m *Memory) Upsert(rs []core.Resource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range rs {
		m.byTypeKey[typeKey{r.Type, r.Key}] = r
	}
}

// DeleteMissing removes all resources of type t whose keys are not in keep.
// Mirrors DB.DeleteMissing so SWR can keep both stores in sync.
func (m *Memory) DeleteMissing(t core.ResourceType, keep map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.byTypeKey {
		if k.Type != t {
			continue
		}
		if _, ok := keep[k.Key]; !ok {
			delete(m.byTypeKey, k)
		}
	}
}

// Len returns the number of top-level resources currently held. Used by
// the TUI to distinguish "cache genuinely empty" from "user hasn't typed
// yet" for the empty-state message.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, r := range m.byTypeKey {
		switch r.Type {
		case core.RTypeBucket, core.RTypeEcsService, core.RTypeEcsTaskDefFamily:
			n++
		}
	}
	return n
}

// All returns a snapshot slice of all top-level resources the TUI should
// search against. Top-level in Phase 1 means: buckets, ecs services, and
// ecs task def families. Folders and objects are excluded here — they are
// searched in the scoped mode, which is Phase 2 territory.
//
// The slice is freshly allocated on every call so callers may sort, filter,
// or otherwise mutate it without locking.
func (m *Memory) All() []core.Resource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]core.Resource, 0, len(m.byTypeKey))
	for _, r := range m.byTypeKey {
		switch r.Type {
		case core.RTypeBucket, core.RTypeEcsService, core.RTypeEcsTaskDefFamily:
			out = append(out, r)
		}
	}
	// Stable order helps the TUI render deterministically when the query is
	// empty. Sort primarily by type priority, then lexicographically by name.
	sort.Slice(out, func(i, j int) bool {
		if pri(out[i].Type) != pri(out[j].Type) {
			return pri(out[i].Type) < pri(out[j].Type)
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// pri returns a ranking priority for stable sort. Lower = earlier in the list.
func pri(t core.ResourceType) int {
	switch t {
	case core.RTypeBucket:
		return 0
	case core.RTypeEcsService:
		return 1
	case core.RTypeEcsTaskDefFamily:
		return 2
	default:
		return 99
	}
}
