package tui

import (
	"os"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// editorAction identifies what should happen after the editor closes.
type editorAction int

const (
	editorActionNone editorAction = iota
	editorActionLambdaInvoke
	editorActionSSMUpdate
)

// msgEditorClosed is emitted after the user saves and exits $EDITOR.
// The handler reads the temp file at pendingEditorPath, dispatches the
// follow-up based on m.pendingEditorAction, then cleans up.
type msgEditorClosed struct {
	Err error
}

// openEditorCmd suspends the TUI, opens the file at `path` in the
// user's $EDITOR (falling back to vi on Unix, notepad on Windows),
// waits for the editor to exit, then emits msgEditorClosed so the
// handler can read the result.
func openEditorCmd(path string) tea.Cmd {
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
		return msgEditorClosed{Err: err}
	})
}
