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
)

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

		type subtaskResult struct {
			typ core.ResourceType
			rs  []core.Resource
			err error
		}

		// Run the three subtasks sequentially for Phase 1. Interleaved
		// updates are a Phase 2 improvement; Phase 1 prioritizes
		// simplicity and determinism.
		subtasks := []func() subtaskResult{
			func() subtaskResult {
				rs, err := awss3.ListBuckets(ctx, ac)
				return subtaskResult{core.RTypeBucket, rs, err}
			},
			func() subtaskResult {
				rs, err := awsecs.ListServices(ctx, ac)
				return subtaskResult{core.RTypeEcsService, rs, err}
			},
			func() subtaskResult {
				rs, err := awsecs.ListTaskDefFamilies(ctx, ac)
				return subtaskResult{core.RTypeEcsTaskDefFamily, rs, err}
			},
		}
		for _, run := range subtasks {
			res := run()
			if res.err == nil {
				persist(ctx, db, mem, res.typ, res.rs)
			}
		}
		cancel()
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
