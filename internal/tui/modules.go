package tui

import (
	"strings"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/module"
)

// moduleForAlias finds the module registered under the given alias or
// scope prefix. Returns (nil, false) when no module owns the alias or
// the registry is empty.
func (m Model) moduleForAlias(alias string) (module.Module, bool) {
	if m.registry == nil {
		return nil, false
	}
	return m.registry.Lookup(alias)
}

// moduleForID finds the module registered under the exact manifest ID.
func (m Model) moduleForID(id string) (module.Module, bool) {
	if m.registry == nil {
		return nil, false
	}
	return m.registry.Get(id)
}

// resourceFromRow synthesises the core.Resource shape prefs
// understands for a module-owned row. Phase-3 reshapes the prefs
// tables to (packageID, key) tuples and this synthesis goes away.
//
// Type stays at the zero value (ResourceType(0)) — the key carries
// the PackageID prefix so collisions across modules can't happen.
func resourceFromRow(r core.Row) core.Resource {
	return core.Resource{
		Type:        core.ResourceType(0),
		Key:         r.PackageID + ":" + r.Key,
		DisplayName: r.Name,
		Meta:        r.Meta,
	}
}

// scopeFromInput returns (scope, rest, true) when the input is of the
// form "<alias>:<rest>" AND the alias resolves to a module. Returns
// ("", input, false) when no module match is found — callers fall
// back to the legacy parser in that case.
func (m Model) scopeFromInput(input string) (scope, rest string, ok bool) {
	idx := strings.Index(input, ":")
	if idx < 0 {
		return "", input, false
	}
	alias := input[:idx]
	if _, ok := m.moduleForAlias(alias); !ok {
		return "", input, false
	}
	return alias, input[idx+1:], true
}
