package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awslogs "github.com/wagnermattei/better-aws-cli/internal/awsctx/logs"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
	"github.com/wagnermattei/better-aws-cli/internal/search"
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

// scopedSearchCmd runs the scoped (bucket/prefix) search behind modeSearch
// when the input contains a `/`. It reads the SQLite cache for first
// paint, fires a live ListObjectsV2 in parallel, persists every live
// result to bucket_contents, merges cache + live into a single slice, and
// returns the whole thing via msgScopedResults.
//
// The merge rule: live results win per (bucket, key) because they are
// authoritative for size/mtime. Results are ordered by the search.Prefix
// helper to match the TUI's display expectations.
func scopedSearchCmd(ac *awsctx.Context, db *index.DB, query string) tea.Cmd {
	return func() tea.Msg {
		scope := search.ParseScope(query)
		if scope.Bucket == "" {
			return msgScopedResults{query: query, results: nil}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 1. Cache read — fast, authoritative for first paint.
		cached, _ := db.QueryBucketContents(ctx, scope.Bucket, scope.Prefix)

		// 2. Live ListObjectsV2 at the narrowest prefix S3 can filter on.
		// Concatenating Prefix+Leaf lets S3 do the filtering server-side
		// (so typing narrows each request) and capping at
		// MaxDisplayedResults keeps first-paint latency flat even for
		// buckets with millions of keys.
		livePrefix := scope.Prefix + scope.Leaf
		live, err := awss3.ListAtPrefix(ctx, ac, scope.Bucket, livePrefix, MaxDisplayedResults)
		if err != nil {
			// On live failure, return whatever was in the cache so the
			// UI still shows something. Phase 4's error toast will
			// surface the error itself. search.Prefix both filters by
			// the leaf and attaches the highlight span.
			return msgScopedResults{
				query:   query,
				results: search.Prefix(scope.Leaf, cached, MaxDisplayedResults),
			}
		}

		// 3. Persist the live results opportunistically.
		_ = db.UpsertBucketContents(ctx, scope.Bucket, live)

		// 4. Merge: live keys overwrite cache keys, then prefix-match
		//    against the leaf in a single pass.
		merged := mergeByKey(cached, live)
		results := search.Prefix(scope.Leaf, merged, MaxDisplayedResults)
		return msgScopedResults{query: query, results: results}
	}
}

// mergeByKey merges two resource slices, preferring entries from `b` when
// both slices contain the same Key. Returns a new slice; inputs are not
// mutated.
func mergeByKey(a, b []core.Resource) []core.Resource {
	byKey := make(map[string]int, len(a)+len(b))
	out := make([]core.Resource, 0, len(a)+len(b))
	for _, r := range a {
		byKey[r.Key] = len(out)
		out = append(out, r)
	}
	for _, r := range b {
		if i, ok := byKey[r.Key]; ok {
			out[i] = r
			continue
		}
		byKey[r.Key] = len(out)
		out = append(out, r)
	}
	return out
}

// msgTaskDefResolved carries the result of a DescribeFamily call for the
// given family. The handler populates m.taskDefDetails[family] so the
// Details view and action commands can read it.
type msgTaskDefResolved struct {
	family  string
	details *awsecs.TaskDefDetails
	err     error
}

// resolveTaskDefCmd kicks off a DescribeFamily call for the given family.
// The handler for msgTaskDefResolved stores the result in
// m.taskDefDetails so the Details view's ARN row and the Tail Logs
// action can read it.
func resolveTaskDefCmd(ac *awsctx.Context, family string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := awsecs.DescribeFamily(ctx, ac, family)
		return msgTaskDefResolved{family: family, details: d, err: err}
	}
}

// msgTailStarted marks a successful StartLiveTail call. The handler
// stashes the stream on the model and schedules the first tailLogsNextCmd.
type msgTailStarted struct {
	stream *awslogs.TailStream
	err    error
}

// msgTailEvent carries one streamed log event to the Update loop. An
// event with Message=="" and Err!=nil means the stream terminated.
type msgTailEvent struct {
	ev  awslogs.TailEvent
	err error
	eof bool
}

// tailLogsStartCmd opens the StartLiveTail stream for the given log
// group. The returned tea.Cmd emits msgTailStarted; the Update handler
// stores the stream and schedules the first msgTailEvent pump.
func tailLogsStartCmd(ac *awsctx.Context, group, account string) tea.Cmd {
	return func() tea.Msg {
		stream, err := awslogs.StartLiveTail(context.Background(), ac, group, account)
		return msgTailStarted{stream: stream, err: err}
	}
}

// tailLogsNextCmd blocks until the next event arrives on the stream,
// then emits it as msgTailEvent. The handler schedules another
// tailLogsNextCmd to keep the pump going. When the stream closes the
// final message carries eof=true.
func tailLogsNextCmd(stream *awslogs.TailStream) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-stream.Events:
			if !ok {
				return msgTailEvent{eof: true}
			}
			return msgTailEvent{ev: ev}
		case err := <-stream.Err:
			return msgTailEvent{err: err, eof: true}
		}
	}
}
