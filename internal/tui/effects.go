package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
)

// modelHost implements effect.Host against a *Model. The reducer
// mutates the model indirectly through these methods so effect/
// stays free of bubbletea/model concerns.
type modelHost struct {
	m *Model
	// deferred is a list of tea.Cmds queued by Host methods that need
	// the bubbletea run loop (Async runner, editor opener). Returned
	// alongside the mutated model from ApplyEffect.
	deferred []tea.Cmd
}

func (h *modelHost) ShowToast(msg string, lvl effect.Level, durNS int64) {
	dur := time.Duration(durNS)
	switch lvl {
	case effect.LevelError:
		h.m.toast = newErrorToast(msg)
	case effect.LevelSuccess:
		if dur == 0 {
			dur = 3 * time.Second
		}
		h.m.toast = newToast(msg, dur)
	default:
		if dur == 0 {
			dur = 2 * time.Second
		}
		h.m.toast = newToast(msg, dur)
	}
}

func (h *modelHost) SetClipboard(text string) error {
	return copyToClipboard(text)
}

func (h *modelHost) OpenBrowser(url string) error {
	return openInBrowser(url)
}

func (h *modelHost) SetInFlight(label string) {
	h.m.inFlight = true
	h.m.inFlightLabel = label
}

func (h *modelHost) ClearInFlight() {
	h.m.inFlight = false
	h.m.inFlightLabel = ""
}

func (h *modelHost) ArmConfirm(prompt string, onYes effect.Effect) {
	h.m.toast = newToast(prompt+" — press y to confirm, any other key to cancel", 30*time.Second)
	h.m.pendingConfirmFn = func(m Model) (Model, tea.Cmd) {
		return ApplyEffect(m, onYes)
	}
}

func (h *modelHost) EnterTailLogs(logGroup string) {
	h.m.mode = modeTailLogs
	h.m.tailGroup = logGroup
	h.m.tailLines = nil
	h.m.tailFilter = ""
	h.m.tailFilterEditing = false
	h.deferred = append(h.deferred, startTailLogsEffectCmd(h.m.awsCtx, logGroup))
}

func (h *modelHost) SetModuleState(packageID string, state effect.State) {
	if h.m.moduleState == nil {
		h.m.moduleState = make(map[string]effect.State)
	}
	h.m.moduleState[packageID] = state
}

func (h *modelHost) UpsertCacheRows(rows []effect.Row) {
	if h.m.moduleCache == nil || len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	coreRows := make([]core.Row, len(rows))
	for i, r := range rows {
		coreRows[i] = core.Row{
			PackageID: r.PackageID,
			Key:       r.Key,
			Name:      r.Name,
			Meta:      r.Meta,
		}
	}
	_ = h.m.moduleCache.Upsert(ctx, coreRows)
}

func (h *modelHost) SetLazyDetails(packageID, key string, lazy map[string]string) {
	if h.m.moduleLazy == nil {
		h.m.moduleLazy = make(map[lazyDetailKey]map[string]string)
	}
	// During migration we reuse lazyDetailKey; the Type field is
	// overloaded to carry "0 = modules era". Phase 3 retires the old
	// type entirely.
	k := lazyDetailKey{Type: core.ResourceType(0), Key: packageID + ":" + key}
	h.m.moduleLazy[k] = lazy
}

// ApplyEffect runs the reducer against a Model and returns the new
// Model + the batched tea.Cmd for any Async/editor/tail work queued
// along the way. Entry point for both module HandleSearch result
// effects and Action Run effects.
func ApplyEffect(m Model, eff effect.Effect) (Model, tea.Cmd) {
	host := &modelHost{m: &m}
	effect.Reduce(eff, host, asyncRunner(host), editorOpener(host))
	if len(host.deferred) == 0 {
		return m, nil
	}
	return m, tea.Batch(host.deferred...)
}

// asyncRunner returns an AsyncRunner bound to the given host. The
// runner queues a tea.Cmd that runs Fn in a goroutine; the Cmd's
// message is msgEffectDone carrying the next Effect to reduce.
func asyncRunner(h *modelHost) effect.AsyncRunner {
	return func(label string, fn func() effect.Effect) {
		h.deferred = append(h.deferred, func() tea.Msg {
			next := fn()
			return msgEffectDone{next: next}
		})
	}
}

// editorOpener returns an EditorOpener bound to the given host.
// Opens $EDITOR via tea.ExecProcess through the existing openEditorCmd
// helper; on save, OnSave is called and its returned Effect is queued.
func editorOpener(h *modelHost) effect.EditorOpener {
	return func(prefill []byte, onSave func(content []byte) effect.Effect) {
		h.m.pendingEditorEffectOnSave = onSave
		cmd := openEditorWithPrefillCmd(prefill, func(content []byte) tea.Msg {
			return msgEffectDone{next: onSave(content)}
		})
		h.deferred = append(h.deferred, cmd)
	}
}

// msgEffectDone carries the next Effect produced by an Async or an
// Editor callback. Routed through the main Update loop which
// re-reduces it via ApplyEffect.
type msgEffectDone struct {
	next effect.Effect
}

// handleEffectDone is wired into update.go alongside the other
// message handlers.
func (m Model) handleEffectDone(msg msgEffectDone) (Model, tea.Cmd) {
	nm, cmd := ApplyEffect(m, msg.next)
	// Clear inFlight whenever an Async lands.
	nm.inFlight = false
	nm.inFlightLabel = ""
	return nm, cmd
}

// Placeholders — these helpers get their real bodies as Phase 2
// migrates modules that need them. Fail loudly if called prematurely.
func startTailLogsEffectCmd(ac interface{}, group string) tea.Cmd {
	_ = ac
	return func() tea.Msg {
		return msgEffectDone{next: effect.Toast{Message: fmt.Sprintf("tail stub: %s", group), Level: effect.LevelWarning}}
	}
}

func openEditorWithPrefillCmd(prefill []byte, onClose func(content []byte) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return onClose(prefill)
	}
}
