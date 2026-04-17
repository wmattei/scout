package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	awss3 "github.com/wmattei/scout/internal/awsctx/s3"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/prefs"
	"github.com/wmattei/scout/internal/search"
	"github.com/wmattei/scout/internal/services"
)

// refreshServiceCmd fires a live fetch for a single resource type,
// persists the result, and emits msgResourcesUpdated. Used by the
// manual service-scope feature: the first time a session enters
// "<alias>:", the TUI fires this to populate the in-memory index with
// fresh data for just that type. The full top-level refresh used to
// run on launch but was retired in favour of the explicit
// `scout preload <service>` subcommand — every refresh now
// either user-typed (a service scope) or user-invoked (the
// subcommand).
func refreshServiceCmd(ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		p, ok := services.Get(t)
		if !ok {
			return msgResourcesUpdated{}
		}
		rs, err := p.ListAll(ctx, ac, awsctx.ListOptions{})
		if err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		if err := index.Persist(ctx, db, mem, t, rs); err != nil {
			return msgResourcesUpdated{errors: []string{err.Error()}}
		}
		return msgResourcesUpdated{}
	}
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
			// UI still shows something and forward the error text so
			// the Update handler can pop a toast.
			return msgScopedResults{
				query:   query,
				results: search.Prefix(scope.Leaf, cached, MaxDisplayedResults),
				err:     "scoped search failed: " + err.Error(),
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

// msgLazyDetailsResolved carries the result of a generic lazy-detail
// resolution started from the Enter handler. The handler stores the
// returned map in m.lazyDetails keyed by msg.key.
type msgLazyDetailsResolved struct {
	key     lazyDetailKey
	details map[string]string
	err     error
}

// resolveLazyDetailsCmd dispatches a provider's ResolveDetails as a
// tea.Cmd. The caller is responsible for marking
// m.lazyDetailsState[key] = lazyStateInFlight before returning this
// command from Update — the message handler flips it to
// lazyStateResolved on completion.
func resolveLazyDetailsCmd(ac *awsctx.Context, p services.Provider, r core.Resource) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		details, err := p.ResolveDetails(ctx, ac, r)
		return msgLazyDetailsResolved{
			key:     lazyDetailKey{Type: r.Type, Key: r.Key},
			details: details,
			err:     err,
		}
	}
}

// msgTailStarted marks a successful StartLiveTail call. The handler
// stashes the stream on the model and schedules the first tailLogsNextCmd.
// historicalLines carries pre-formatted lines from GetRecentEvents so the
// viewport isn't empty while the user waits for the first live event.
type msgTailStarted struct {
	stream          *awslogs.TailStream
	historicalLines []string
	err             error
}

// msgTailEvent carries one streamed log event to the Update loop. An
// event with Message=="" and Err!=nil means the stream terminated.
type msgTailEvent struct {
	ev  awslogs.TailEvent
	err error
	eof bool
}

// tailLogsStartCmd first fetches the most recent 50 log events from
// the log group (last 30 minutes), then opens the StartLiveTail
// stream. The historical events are pre-formatted and carried on
// msgTailStarted so the handler can seed the viewport before the
// first live event arrives. A visual divider line separates old logs
// from new ones in the viewport.
func tailLogsStartCmd(ac *awsctx.Context, group, account string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// 1. Fetch recent historical events (best-effort).
		var historical []string
		if events, err := awslogs.GetRecentEvents(ctx, ac, group, 50, 30*time.Minute); err == nil && len(events) > 0 {
			for _, ev := range events {
				historical = append(historical, formatTailLine(ev))
			}
		}

		// 2. Open the live-tail stream.
		stream, err := awslogs.StartLiveTail(ctx, ac, group, account)
		return msgTailStarted{stream: stream, historicalLines: historical, err: err}
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

// msgSwitcherCommitted carries the outcome of a profile/region swap.
// On success, the new Context replaces m.awsCtx, the new DB handle
// replaces m.db, and the in-memory index is swapped to the freshly
// loaded cache. On failure, the old state is preserved and an error
// toast is raised.
type msgSwitcherCommitted struct {
	ctx        *awsctx.Context
	db         *index.DB
	memory     *index.Memory
	prefs      *prefs.DB
	prefsState *prefs.State
	err        error
}

// commitSwitcherCmd runs the heavy lifting of a profile/region swap
// off the UI goroutine: load a new aws.Config via ResolveForProfile,
// open the matching SQLite file, LoadAll() into a fresh Memory, and
// return everything via msgSwitcherCommitted. The UI handler does
// the final state assignment so the swap is atomic from the Update
// loop's perspective.
func commitSwitcherCmd(profile, region string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		newCtx, err := awsctx.ResolveForProfile(ctx, profile, region)
		if err != nil {
			return msgSwitcherCommitted{err: err}
		}
		newDB, err := index.Open(newCtx.Profile, newCtx.Region)
		if err != nil {
			return msgSwitcherCommitted{err: err}
		}
		cached, err := newDB.LoadAll(ctx)
		if err != nil {
			_ = newDB.Close()
			return msgSwitcherCommitted{err: err}
		}
		mem := index.NewMemory()
		mem.Load(cached)

		newPrefs, newPrefsState, err := prefs.Open(newCtx.Profile, newCtx.Region)
		if err != nil {
			_ = newDB.Close()
			return msgSwitcherCommitted{err: err}
		}
		return msgSwitcherCommitted{
			ctx:        newCtx,
			db:         newDB,
			memory:     mem,
			prefs:      newPrefs,
			prefsState: newPrefsState,
		}
	}
}
