// Package effect defines declarative intents emitted by module actions
// and search handlers. The reducer (internal/effect/reducer.go)
// converts effects into state changes and tea.Cmds. Module code stays
// free of bubbletea imports; the reducer is the only place that
// bridges to the bubbletea world.
package effect

import (
	"time"

	"github.com/wmattei/scout/internal/core"
)

// Effect is the sealed union. Every concrete effect implements
// effect() with a blank body — Go's idiomatic discriminated-union
// pattern.
type Effect interface{ effect() }

// Copy writes Text to the clipboard and shows a toast labelled Label.
type Copy struct {
	Text  string
	Label string
}

func (Copy) effect() {}

// Browser opens URL via the OS shell-out (open / xdg-open).
type Browser struct {
	URL string
}

func (Browser) effect() {}

// Toast shows a message with the given level and duration. 0 duration
// uses the level default (2s info, 3s success, 4s error).
type Toast struct {
	Message  string
	Level    Level
	Duration time.Duration
}

func (Toast) effect() {}

// Editor opens $EDITOR prefilled with Prefill. On save (mtime changed)
// OnSave is invoked with the file contents; its returned Effect is
// reduced. If the user quits without saving, OnSave is skipped and a
// "cancelled" toast is shown.
type Editor struct {
	Prefill []byte
	OnSave  func(content []byte) Effect
}

func (Editor) effect() {}

// Confirm arms a y/n gate. On 'y', OnYes is reduced. Any other key
// cancels with a neutral toast.
type Confirm struct {
	Prompt string
	OnYes  Effect
}

func (Confirm) effect() {}

// TailLogs enters the log-tail mode against the given log group.
type TailLogs struct {
	LogGroup string
}

func (TailLogs) effect() {}

// Async runs Fn off the UI goroutine with inFlight set to Label.
// Fn's returned Effect is reduced when the call completes.
type Async struct {
	Label string
	Fn    func() Effect
}

func (Async) effect() {}

// Batch reduces each child effect sequentially in one update tick.
// Used when a single action needs to do multiple things (e.g. toast
// + upsert + re-render).
type Batch struct {
	Effects []Effect
}

func (Batch) effect() {}

// SetState stores module-owned opaque state for PackageID. The State
// survives until the user leaves module mode (e.g. escapes back to
// top-level) or the AWS context switches.
type SetState struct {
	PackageID string
	State     State
}

func (SetState) effect() {}

// State is the module-owned opaque state blob. Core persists it on
// Model; modules marshal whatever they need into Bytes.
type State struct {
	Bytes []byte
}

// UpsertCache persists Rows to the shared cache. Used by HandleSearch
// Async effects when live AWS results come back.
type UpsertCache struct {
	Rows []core.Row
}

func (UpsertCache) effect() {}

// SetLazy stores a resolved-details map on Model, keyed by
// (PackageID, Key). Rendered by BuildDetails on the next frame.
type SetLazy struct {
	PackageID string
	Key       string
	Lazy      map[string]string
}

func (SetLazy) effect() {}

// None is a no-op effect. Useful as the zero value for callbacks that
// might not always need to do something.
type None struct{}

func (None) effect() {}

// OpenVirtualDetails opens the Details page for a virtual row owned
// by a module. The module is expected to recognise Key and synthesize
// a core.Row for it in BuildDetails. Used by Automation to open an
// execution detail view from a runbook's Events zone.
type OpenVirtualDetails struct {
	PackageID string
	Key       string
	Name      string
}

func (OpenVirtualDetails) effect() {}

// Tick fires Then after After elapses. Used by modules that need
// recurring polling (Automation execution status). Implemented via
// tea.Tick in the reducer Host.
type Tick struct {
	After time.Duration
	Then  Effect
}

func (Tick) effect() {}
