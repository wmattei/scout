package tui

import (
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// editorAction identifies what should happen after the editor closes.
type editorAction int

const (
	editorActionNone editorAction = iota
	editorActionLambdaInvoke
	editorActionSSMUpdate
	editorActionSecretUpdate
	editorActionAutomationRun
)

// msgEditorClosed is emitted after the user saves and exits $EDITOR.
// The handler reads the temp file at pendingEditorPath, dispatches the
// follow-up based on m.pendingEditorAction, then cleans up. It carries
// the file mtime recorded BEFORE the editor opened so the handler can
// detect "user quit without saving" by comparing against the current
// mtime — if they match, no save happened.
type msgEditorClosed struct {
	Err       error
	MtimePre  time.Time // mtime of temp file before editor opened
}

// openEditorCmd records the temp file's mtime, suspends the TUI,
// opens the file in $EDITOR, waits for the editor to exit, and emits
// msgEditorClosed with the pre-open mtime so the handler can detect
// whether the user actually saved.
func openEditorCmd(path string) tea.Cmd {
	// Capture mtime before the editor touches the file.
	var mtimePre time.Time
	if info, err := os.Stat(path); err == nil {
		mtimePre = info.ModTime()
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
		return msgEditorClosed{Err: err, MtimePre: mtimePre}
	})
}
