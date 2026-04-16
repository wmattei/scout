package index

import (
	"context"
	"fmt"

	"github.com/wmattei/scout/internal/core"
)

// Persist applies a diff-patch refresh of the given resource type to
// both the in-memory index and the on-disk SQLite cache. New rows are
// upserted, rows of type t that are NOT present in rs are deleted (so
// resources removed from AWS since the last refresh disappear from the
// cache too). Writes go to the in-memory index first so the UI snaps
// immediately, then to SQLite.
//
// Used by both the TUI (refreshServiceCmd, the lazy first-entry fetch
// for service-scope mode) and the cmd-line preload subcommand. The
// returned error reports the first SQLite failure; callers that don't
// care about persistence durability can ignore it (the in-memory
// upsert has already happened by then).
func Persist(ctx context.Context, db *DB, mem *Memory, t core.ResourceType, rs []core.Resource) error {
	keep := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		keep[r.Key] = struct{}{}
	}
	mem.Upsert(rs)
	mem.DeleteMissing(t, keep)

	if err := db.UpsertResources(ctx, rs); err != nil {
		return fmt.Errorf("upserting %s resources: %w", t, err)
	}
	if err := db.DeleteMissing(ctx, t, keep); err != nil {
		return fmt.Errorf("deleting missing %s resources: %w", t, err)
	}
	return nil
}
