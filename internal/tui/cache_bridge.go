package tui

import (
	"context"

	"github.com/wmattei/scout/internal/cache"
	"github.com/wmattei/scout/internal/debuglog"
)

// reopenModuleCache closes the current module cache (if any) and
// opens a fresh one for the given (profile, region). Runs the orphan
// purge synchronously against the live registry. Swallows errors
// into the debug log — the TUI stays bootable even when the module
// cache is unavailable; module HandleSearch falls back to nil rows.
func (m *Model) reopenModuleCache(ctx context.Context, profile, region string) {
	if m.moduleCache != nil {
		_ = m.moduleCache.Close()
		m.moduleCache = nil
	}
	if profile == "" || region == "" || m.registry == nil {
		return
	}
	db, err := cache.Open(profile, region)
	if err != nil {
		debuglog.Logger().Warn("reopen module cache", "err", err)
		return
	}
	if err := db.PurgeOrphans(ctx, m.registry.IDs()); err != nil {
		debuglog.Logger().Warn("orphan purge", "err", err)
	}
	m.moduleCache = db
}
