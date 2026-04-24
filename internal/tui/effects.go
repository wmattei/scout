package tui

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

// moduleContextFor builds the module.Context passed into a module's
// entry points. Mirrors the current moduleState snapshot so modules
// can read their own state inside BuildDetails, HandleSearch, and
// Action.Run closures.
func (m Model) moduleContextFor(packageID string) module.Context {
	return module.Context{
		AWSCtx: m.awsCtx,
		Cache:  m.moduleCache,
		State:  m.moduleState[packageID],
	}
}

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
	h.deferred = append(h.deferred, startTailLogsEffectCmd(h.m.awsCtx, logGroup, h.m.account))
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

func (h *modelHost) OpenVirtualDetails(packageID, key, name string) {
	h.m.virtualRow = &core.Row{PackageID: packageID, Key: key, Name: name}
	h.m.mode = modeDetails
}

func (h *modelHost) Tick(after time.Duration, next effect.Effect) {
	h.deferred = append(h.deferred, tea.Tick(after, func(time.Time) tea.Msg {
		return msgEffectDone{next: next}
	}))
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
// Opens $EDITOR via tea.ExecProcess; on exit, reads the temp file and
// invokes OnSave, whose returned Effect is reduced via msgEffectDone.
func editorOpener(h *modelHost) effect.EditorOpener {
	return func(prefill []byte, onSave func(content []byte) effect.Effect) {
		h.m.pendingEditorEffectOnSave = onSave
		h.deferred = append(h.deferred, openEditorWithPrefillCmd(prefill, onSave))
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

// startTailLogsEffectCmd delegates to the existing tailLogsStartCmd
// helper used by the legacy action_tail.go path. The resulting
// msgTailStarted / msgTailEvent flow through handleTailStarted /
// handleTailEvent in update_messages.go, which seed m.tailStream,
// m.tailLines, and the viewport — no module-specific plumbing needed.
func startTailLogsEffectCmd(ac *awsctx.Context, group, account string) tea.Cmd {
	return tailLogsStartCmd(ac, group, account)
}

// openEditorWithPrefillCmd writes prefill to a temp file, suspends the
// TUI via tea.ExecProcess while $EDITOR runs, and on editor exit reads
// the file back, hands the content to onSave, and emits
// msgEffectDone with the returned Effect. Errors along the way become
// error toasts via the same msgEffectDone channel so the reducer path
// handles them uniformly.
func openEditorWithPrefillCmd(prefill []byte, onSave func([]byte) effect.Effect) tea.Cmd {
	path, err := writeTempFile(prefill)
	if err != nil {
		return func() tea.Msg {
			return msgEffectDone{next: effect.Toast{
				Message: "editor temp file failed: " + err.Error(),
				Level:   effect.LevelError,
			}}
		}
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		switch runtime.GOOS {
		case "windows":
			editor = "notepad"
		default:
			editor = "vi"
		}
	}
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return msgEffectDone{next: effect.Toast{
				Message: "editor failed: " + err.Error(),
				Level:   effect.LevelError,
			}}
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return msgEffectDone{next: effect.Toast{
				Message: "read after editor failed: " + rerr.Error(),
				Level:   effect.LevelError,
			}}
		}
		return msgEffectDone{next: onSave(content)}
	})
}

func writeTempFile(body []byte) (string, error) {
	f, err := os.CreateTemp("", "scout-edit-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
