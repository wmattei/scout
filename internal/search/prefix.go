package search

import (
	"sort"
	"strings"

	"github.com/wmattei/scout/internal/core"
)

// Prefix runs a case-sensitive prefix match of `query` against each
// resource in `all`, returning up to `limit` results sorted with folders
// before objects and otherwise lexicographically. MatchedRunes on each
// result spans the leading prefix positions so the TUI can render the
// matching chars in the highlight style.
//
// An empty query returns everything up to `limit` unranked — useful
// when the user has just drilled into a bucket and hasn't typed anything
// beyond the trailing `/`.
func Prefix(query string, all []core.Resource, limit int) []Result {
	var matched []core.Resource
	if query == "" {
		matched = make([]core.Resource, len(all))
		copy(matched, all)
	} else {
		matched = make([]core.Resource, 0, len(all))
		for _, r := range all {
			if strings.HasPrefix(r.DisplayName, query) {
				matched = append(matched, r)
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		// Folders before objects.
		ti := folderFirst(matched[i].Type)
		tj := folderFirst(matched[j].Type)
		if ti != tj {
			return ti < tj
		}
		return matched[i].DisplayName < matched[j].DisplayName
	})

	// Precompute the leading-prefix highlight span once; every match shares
	// the same [0..len(query)) byte positions.
	var matchPositions []int
	if query != "" {
		matchPositions = make([]int, 0, len(query))
		for i := 0; i < len(query); i++ {
			matchPositions = append(matchPositions, i)
		}
	}

	upto := minInt(limit, len(matched))
	out := make([]Result, 0, upto)
	for i := 0; i < upto; i++ {
		out = append(out, Result{
			Resource:     matched[i],
			MatchedRunes: matchPositions,
			Score:        0,
		})
	}
	return out
}

// folderFirst gives folders priority 0 and everything else priority 1.
func folderFirst(t core.ResourceType) int {
	if t == core.RTypeFolder {
		return 0
	}
	return 1
}
