package index

import (
	"sort"
	"sync"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// topLevelTypes is the set of resource types that Memory.All() should
// return. The TUI layer wires this up at startup via SetTopLevelTypes
// (see cmd/better-aws/main.go); index can't import the services
// registry directly because services depends on internal/awsctx,
// which depends on... etc — a cycle.
var topLevelTypes = []core.ResourceType{
	core.RTypeBucket,
	core.RTypeEcsService,
	core.RTypeEcsTaskDefFamily,
}

var topLevelPriority = map[core.ResourceType]int{
	core.RTypeBucket:           0,
	core.RTypeEcsService:       1,
	core.RTypeEcsTaskDefFamily: 2,
}

// SetTopLevelTypes overrides the default list of top-level resource
// types. The new list replaces the hardcoded default. Callers also
// pass a per-type priority map for stable sort ordering. Calling with
// nil for either argument restores the hardcoded defaults.
func SetTopLevelTypes(types []core.ResourceType, priority map[core.ResourceType]int) {
	if types == nil {
		topLevelTypes = []core.ResourceType{
			core.RTypeBucket,
			core.RTypeEcsService,
			core.RTypeEcsTaskDefFamily,
		}
	} else {
		topLevelTypes = append([]core.ResourceType{}, types...)
	}
	if priority == nil {
		topLevelPriority = map[core.ResourceType]int{
			core.RTypeBucket:           0,
			core.RTypeEcsService:       1,
			core.RTypeEcsTaskDefFamily: 2,
		}
	} else {
		topLevelPriority = make(map[core.ResourceType]int, len(priority))
		for k, v := range priority {
			topLevelPriority[k] = v
		}
	}
}

// isTopLevelType reports whether the given type is in the current
// top-level set.
func isTopLevelType(t core.ResourceType) bool {
	for _, tl := range topLevelTypes {
		if tl == t {
			return true
		}
	}
	return false
}

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

// ByType returns a snapshot slice of every top-level resource matching
// the given type. Used by the service-scope search feature to restrict
// fuzzy matching to a single resource type without touching All(). The
// result is sorted lexicographically by DisplayName for deterministic
// ordering.
func (m *Memory) ByType(t core.ResourceType) []core.Resource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]core.Resource, 0)
	for _, r := range m.byTypeKey {
		if r.Type == t {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

// Len returns the number of top-level resources currently held. Used by
// the TUI to distinguish "cache genuinely empty" from "user hasn't typed
// yet" for the empty-state message.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, r := range m.byTypeKey {
		if isTopLevelType(r.Type) {
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
		if isTopLevelType(r.Type) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := topLevelPriority[out[i].Type], topLevelPriority[out[j].Type]
		if pi != pj {
			return pi < pj
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}

