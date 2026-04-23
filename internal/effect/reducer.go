package effect

import (
	"time"

	"github.com/wmattei/scout/internal/core"
)

// Host is the callback surface the reducer uses to mutate UI-owned
// state. Implemented by the tui package's Model-wrapper. Keeping this
// as an interface lets effect/ stay bubbletea-free.
type Host interface {
	ShowToast(msg string, lvl Level, dur int64) // dur in nanoseconds; 0 = default
	SetClipboard(text string) error
	OpenBrowser(url string) error
	SetInFlight(label string)
	ClearInFlight()
	ArmConfirm(prompt string, onYes Effect)
	EnterTailLogs(logGroup string)
	SetModuleState(packageID string, state State)
	UpsertCacheRows(rows []Row)
	SetLazyDetails(packageID, key string, lazy map[string]string)
	OpenVirtualDetails(packageID, key, name string)
	Tick(after time.Duration, next Effect)
}

// Row re-exports core.Row so the Host interface stays importable by
// tui/ without a core import cycle. Concrete conversion happens in
// the reducer.
type Row = struct {
	PackageID string
	Key       string
	Name      string
	Meta      map[string]string
}

// AsyncRunner runs an Async.Fn closure and delivers the resulting
// Effect back through the Host's effect-dispatch loop. The tui
// package wires this up with bubbletea's tea.Cmd pattern.
type AsyncRunner func(label string, fn func() Effect)

// EditorOpener opens $EDITOR via tea.ExecProcess-style suspension.
// OnSave is invoked with file contents if the user saved.
type EditorOpener func(prefill []byte, onSave func(content []byte) Effect)

// Reduce applies one effect against the host. Called by the tui
// update loop for every Effect emitted by search handlers and
// actions. Pure w.r.t. the effect arg — all side effects go through
// the Host/AsyncRunner/EditorOpener callbacks.
func Reduce(eff Effect, host Host, runAsync AsyncRunner, openEditor EditorOpener) {
	switch e := eff.(type) {
	case None:
		// intentional no-op
	case Toast:
		host.ShowToast(e.Message, e.Level, int64(e.Duration))
	case Copy:
		if err := host.SetClipboard(e.Text); err != nil {
			host.ShowToast("copy failed: "+err.Error(), LevelError, 0)
			return
		}
		host.ShowToast("copied "+e.Label, LevelSuccess, 0)
	case Browser:
		if err := host.OpenBrowser(e.URL); err != nil {
			host.ShowToast("open failed: "+err.Error(), LevelError, 0)
		}
	case Editor:
		openEditor(e.Prefill, e.OnSave)
	case Confirm:
		host.ArmConfirm(e.Prompt, e.OnYes)
	case TailLogs:
		host.EnterTailLogs(e.LogGroup)
	case Async:
		host.SetInFlight(e.Label)
		runAsync(e.Label, e.Fn)
	case Batch:
		for _, child := range e.Effects {
			Reduce(child, host, runAsync, openEditor)
		}
	case SetState:
		host.SetModuleState(e.PackageID, e.State)
	case UpsertCache:
		host.UpsertCacheRows(toHostRows(e.Rows))
	case SetLazy:
		host.SetLazyDetails(e.PackageID, e.Key, e.Lazy)
	case OpenVirtualDetails:
		host.OpenVirtualDetails(e.PackageID, e.Key, e.Name)
	case Tick:
		host.Tick(e.After, e.Then)
	}
}

func toHostRows(rs []core.Row) []Row {
	out := make([]Row, len(rs))
	for i, r := range rs {
		out[i] = Row{
			PackageID: r.PackageID,
			Key:       r.Key,
			Name:      r.Name,
			Meta:      r.Meta,
		}
	}
	return out
}
