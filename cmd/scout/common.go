package main

import (
	"os"
	"path/filepath"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/services"
)

// Helpers shared between multiple subcommands. Anything used by only
// one command belongs in that command's file.

// cacheDir resolves the per-user scout cache directory, honouring XDG
// first and falling back to ~/.cache/scout. Mirrors the resolver in
// internal/index so subcommands can operate on the directory without
// opening a DB first (e.g. `cache clear`, crash-log writer).
func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "scout"), nil
}

// seedTopLevelTypes populates the index layer's top-level-type registry
// from the services package. Shared by runTUI and preload so both paths
// build identical unified search snapshots. The copy lives here (not
// inside internal/index) so index doesn't need to import services and
// create a cycle.
func seedTopLevelTypes() {
	types := make([]core.ResourceType, 0)
	priority := make(map[core.ResourceType]int)
	for _, p := range services.TopLevel() {
		types = append(types, p.Type())
		priority[p.Type()] = p.SortPriority()
	}
	index.SetTopLevelTypes(types, priority)
}
