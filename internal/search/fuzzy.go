// Package search houses the fuzzy and prefix match engines used by the TUI
// to turn a query string into a ranked, highlight-annotated result list.
//
// The fuzzy engine covers top-level mode; the prefix engine covers scoped
// mode. They share the Result type so the result list renderer doesn't
// need to know which engine produced a row.
package search

import (
	"github.com/sahilm/fuzzy"

	"github.com/wmattei/scout/internal/core"
)

// Result is one row in a search result list, with enough metadata for the
// TUI to render per-character highlight spans.
//
// During the modules cutover, a Result carries EITHER the legacy Resource
// OR a ModuleRow (module-owned core.Row). Exactly one is populated.
// Phase-3 retires Resource and this becomes Row-only.
type Result struct {
	Resource     core.Resource
	ModuleRow    *core.Row // non-nil when this result came from the module cache
	MatchedRunes []int     // byte positions in the name; empty for "no query"
	Score        int       // higher is better; 0 for "no query" baseline
}

// Fuzzy runs a fuzzy match against every resource in `all` and returns the
// top `limit` results ordered by score (descending). An empty query returns
// the input unchanged (already sorted by the caller) and no highlight spans.
func Fuzzy(query string, all []core.Resource, limit int) []Result {
	if query == "" {
		out := make([]Result, 0, minInt(limit, len(all)))
		upto := minInt(limit, len(all))
		for i := 0; i < upto; i++ {
			out = append(out, Result{Resource: all[i]})
		}
		return out
	}

	// sahilm/fuzzy wants a Source interface. Adapt the resource slice.
	src := resSource(all)
	matches := fuzzy.FindFrom(query, src)

	upto := minInt(limit, len(matches))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		m := matches[i]
		out = append(out, Result{
			Resource:     all[m.Index],
			MatchedRunes: m.MatchedIndexes,
			Score:        m.Score,
		})
	}
	return out
}

// resSource adapts []core.Resource to fuzzy.Source so the library can read
// DisplayName for each entry.
type resSource []core.Resource

func (r resSource) String(i int) string { return r[i].DisplayName }
func (r resSource) Len() int            { return len(r) }

// FuzzyOverRows runs the same fuzzy match against []core.Row. Returns
// the top `limit` results; each one has ModuleRow populated (Resource
// stays zero). An empty query returns the first `limit` rows with no
// highlight spans.
func FuzzyOverRows(query string, all []core.Row, limit int) []Result {
	if query == "" {
		upto := minInt(limit, len(all))
		out := make([]Result, 0, upto)
		for i := 0; i < upto; i++ {
			r := all[i]
			out = append(out, Result{ModuleRow: &r})
		}
		return out
	}

	src := rowSource(all)
	matches := fuzzy.FindFrom(query, src)
	upto := minInt(limit, len(matches))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		m := matches[i]
		r := all[m.Index]
		out = append(out, Result{
			ModuleRow:    &r,
			MatchedRunes: m.MatchedIndexes,
			Score:        m.Score,
		})
	}
	return out
}

type rowSource []core.Row

func (r rowSource) String(i int) string { return r[i].Name }
func (r rowSource) Len() int            { return len(r) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
