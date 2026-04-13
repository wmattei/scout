package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
)

// initialResultsCmd produces the first render's results from whatever the
// in-memory index currently holds. It does not hit AWS.
func initialResultsCmd(mem *index.Memory, query string) tea.Cmd {
	return func() tea.Msg {
		results := search.Fuzzy(query, mem.All(), 200)
		return msgResults{results: results}
	}
}

// refreshTopLevelCmd kicks off SWR refresh for buckets + ecs services +
// ecs task-def families concurrently. Each subtask writes its results to
// both the in-memory index and the SQLite cache, then emits
// msgResourcesUpdated so the UI can re-render.
//
// Errors are swallowed for Phase 1 — the user sees a stale cache with no
// indication of what went wrong. Phase 4 adds an error toast.
func refreshTopLevelCmd(ac *awsctx.Context, db *index.DB, mem *index.Memory) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		type subtaskResult struct {
			typ core.ResourceType
			rs  []core.Resource
			err error
		}
		done := make(chan subtaskResult, 3)

		go func() {
			rs, err := awss3.ListBuckets(ctx, ac)
			done <- subtaskResult{core.RTypeBucket, rs, err}
		}()
		go func() {
			rs, err := awsecs.ListServices(ctx, ac)
			done <- subtaskResult{core.RTypeEcsService, rs, err}
		}()
		go func() {
			rs, err := awsecs.ListTaskDefFamilies(ctx, ac)
			done <- subtaskResult{core.RTypeEcsTaskDefFamily, rs, err}
		}()

		for i := 0; i < 3; i++ {
			res := <-done
			if res.err != nil {
				// Phase 4: forward to error toast. For now, drop.
				continue
			}
			persist(ctx, db, mem, res.typ, res.rs)
		}
		return msgResourcesUpdated{}
	}
}

// persist applies a diff-patch: upsert all received resources, then delete
// any resources of this type that were NOT in the fresh set. Writes go to
// the in-memory index first (instant UI snap) and then to SQLite.
func persist(ctx context.Context, db *index.DB, mem *index.Memory, t core.ResourceType, rs []core.Resource) {
	// 1. In-memory: upsert + delete-missing for this type.
	keep := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		keep[r.Key] = struct{}{}
	}
	mem.Upsert(rs)
	mem.DeleteMissing(t, keep)

	// 2. Persist to SQLite.
	_ = db.UpsertResources(ctx, rs)
	_ = db.DeleteMissing(ctx, t, keep)
}

// resolveAccountCmd calls sts:GetCallerIdentity once and reports the account
// ID (or a blank on error) to the TUI.
func resolveAccountCmd(ac *awsctx.Context) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		acct, _ := ac.CallerIdentity(ctx)
		return msgAccount{account: acct}
	}
}
