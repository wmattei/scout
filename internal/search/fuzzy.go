// Package search houses the fuzzy and prefix match engines used by the TUI
// to turn a query string into a ranked, highlight-annotated result list.
//
// Phase 1 only uses the fuzzy engine (top-level mode). Phase 2 adds the
// prefix engine for scoped mode. They share the Result type so the result
// list renderer doesn't need to know which engine produced a row.
package search

import (
	"github.com/sahilm/fuzzy"

	"github.com/wmattei/scout/internal/core"
)

// Result is one row in a search result list, with enough metadata for the
// TUI to render per-character highlight spans.
type Result struct {
	Resource     core.Resource
	MatchedRunes []int // byte positions in DisplayName; empty for "no query"
	Score        int   // higher is better; 0 for "no query" baseline
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
func (r resSource) Len() int             { return len(r) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
