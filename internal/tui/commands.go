package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
	"github.com/wmattei/scout/internal/prefs"
)

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
// On success, the new Context replaces m.awsCtx and the new prefs DB
// handle replaces m.prefs. On failure, the old state is preserved and
// an error toast is raised.
type msgSwitcherCommitted struct {
	ctx        *awsctx.Context
	prefs      *prefs.DB
	prefsState *prefs.State
	// err means the entire switch failed (e.g. aws resolve error).
	// prefsErr means only the prefs DB failed to open; the switch
	// succeeded and the TUI will run with nil prefs this session.
	err      error
	prefsErr error
}

// commitSwitcherCmd runs the heavy lifting of a profile/region swap
// off the UI goroutine: load a new aws.Config via ResolveForProfile
// and open the matching prefs DB. The UI handler does the final state
// assignment so the swap is atomic from the Update loop's perspective.
func commitSwitcherCmd(profile, region string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		newCtx, err := awsctx.ResolveForProfile(ctx, profile, region)
		if err != nil {
			return msgSwitcherCommitted{err: err}
		}

		// prefs.Open failure is non-fatal: the context switch should
		// still complete so the user reaches the new profile/region.
		// The TUI handles nil prefs everywhere and will just disable
		// favorite/recent features for this session.
		newPrefs, newPrefsState, prefsErr := prefs.Open(newCtx.Profile, newCtx.Region)
		return msgSwitcherCommitted{
			ctx:        newCtx,
			prefs:      newPrefs,
			prefsState: newPrefsState,
			prefsErr:   prefsErr,
		}
	}
}
