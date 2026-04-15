package services

import (
	"sort"
	"sync"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// registry is the process-global Provider lookup table. All access
// goes through the package-level functions below so the underlying
// map can stay private.
var (
	mu        sync.RWMutex
	byType    = map[core.ResourceType]Provider{}
	byAlias   = map[string]Provider{}
	insertion []Provider
)

// Register installs p in the registry. Called from each provider
// package's init(). Panics on a duplicate Type to surface
// double-registration mistakes loudly during development. Aliases
// must also be unique; duplicates panic for the same reason.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()

	t := p.Type()
	if existing, ok := byType[t]; ok {
		panic("services: duplicate registration for type " + t.String() + " (existing=" + existing.TagLabel() + ", incoming=" + p.TagLabel() + ")")
	}
	byType[t] = p
	insertion = append(insertion, p)

	for _, alias := range p.Aliases() {
		if existing, ok := byAlias[alias]; ok {
			panic("services: duplicate alias " + alias + " (existing=" + existing.TagLabel() + ", incoming=" + p.TagLabel() + ")")
		}
		byAlias[alias] = p
	}
}

// Get returns the Provider registered under the given resource type.
// The bool is false when no provider is registered — callers should
// treat that as a programmer error and skip rendering / behavior
// rather than crashing.
func Get(t core.ResourceType) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := byType[t]
	return p, ok
}

// MustGet is the same as Get but panics when nothing is registered.
// Use this in code paths where the caller is sure the type is in
// the registry — e.g. inside a per-type rendering function that was
// itself selected by walking registered providers.
func MustGet(t core.ResourceType) Provider {
	p, ok := Get(t)
	if !ok {
		panic("services: no provider registered for type " + t.String())
	}
	return p
}

// Lookup resolves a manual-scope alias (the part before the ":" in
// "s3:prod") to its provider. The bool is false when no provider
// claims the alias.
func Lookup(alias string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := byAlias[alias]
	return p, ok
}

// All returns every registered provider in insertion order. Used by
// help / debug / future "list services" features. Callers must not
// mutate the returned slice.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, len(insertion))
	copy(out, insertion)
	return out
}

// TopLevel returns every registered provider whose IsTopLevel() is
// true, sorted by SortPriority(). Used by index.Memory.All() to
// decide which resource types belong in the unified top-level
// fuzzy index.
func TopLevel() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	var out []Provider
	for _, p := range insertion {
		if p.IsTopLevel() {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SortPriority() < out[j].SortPriority()
	})
	return out
}

// AliasesFor returns every alias registered for a given resource
// type. Convenience wrapper used by help text and the future debug
// view; the same data lives on Provider.Aliases() but this saves a
// Get() call from outside the registry.
func AliasesFor(t core.ResourceType) []string {
	p, ok := Get(t)
	if !ok {
		return nil
	}
	return p.Aliases()
}
