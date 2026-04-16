package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsssm "github.com/wagnermattei/better-aws-cli/internal/awsctx/ssm"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execSSMCopyValue reads the resolved parameter value from the lazy map and
// copies it to the clipboard. If the lazy data hasn't been resolved yet,
// the user is prompted to retry after the details panel finishes loading.
func execSSMCopyValue(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSSMParameter {
		m.toast = newToast("Copy Value is only available for SSM parameters", 3*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	if lazy == nil {
		if m.lazyDetailsState[lazyDetailKey{Type: r.Type, Key: r.Key}] == lazyStateInFlight {
			m.toast = newToast("details still resolving — try again", 2*time.Second)
		} else {
			m.toast = newToast("parameter value not yet resolved — try again", 2*time.Second)
		}
		return m, nil
	}
	value := lazy["value"]
	if err := copyToClipboard(value); err != nil {
		m.toast = newErrorToast(err.Error())
		return m, nil
	}
	m.toast = newToast("value copied", 3*time.Second)
	return m, nil
}

// execSSMUpdateValue opens $EDITOR pre-filled with the current parameter
// value. After the editor closes, ssmUpdateCmd writes the new value back to
// SSM via PutParameter.
func execSSMUpdateValue(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSSMParameter {
		m.toast = newToast("Update Value is only available for SSM parameters", 3*time.Second)
		return m, nil
	}

	lazy := m.lazyDetailsFor(r)
	currentValue := ""
	if lazy != nil {
		currentValue = lazy["value"]
	}

	f, err := os.CreateTemp("", "better-aws-ssm-*.txt")
	if err != nil {
		m.toast = newErrorToast(fmt.Sprintf("create temp file: %v", err))
		return m, nil
	}
	if _, err := f.WriteString(currentValue); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		m.toast = newErrorToast(fmt.Sprintf("write temp file: %v", err))
		return m, nil
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		m.toast = newErrorToast(fmt.Sprintf("close temp file: %v", err))
		return m, nil
	}

	m.pendingEditorAction = editorActionSSMUpdate
	m.pendingEditorPath = f.Name()
	m.pendingEditorResource = m.detailsResource
	return m, openEditorCmd(f.Name())
}

// ssmUpdateCmd reads the new value from content and calls ssm:PutParameter.
// The parameter type is taken from the lazy map so SecureString parameters
// remain encrypted after the update.
func ssmUpdateCmd(ac *awsctx.Context, r core.Resource, content []byte) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Resolve the parameter type from the resource Meta. The lazy map
		// is not available inside the Cmd goroutine, so we fall back to
		// the Meta field captured at listing time — it carries the type.
		paramType := r.Meta["type"]
		if paramType == "" {
			paramType = "String"
		}

		newValue := string(content)
		if err := awsssm.PutParameter(ctx, ac, r.Key, newValue, paramType); err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("update failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "parameter updated", err: nil}
	}
}
