package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awssm "github.com/wmattei/scout/internal/awsctx/secretsmanager"
	"github.com/wmattei/scout/internal/core"
)

// execSecretRevealValue toggles whether the resolved secret value is
// displayed unmasked in the Details view. The toggle state lives in the
// lazyDetails map under SensitiveRevealedKey and is wiped whenever the
// lazy map is refetched — so re-entering Details or refreshing always
// re-hides the value.
func execSecretRevealValue(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSecretsManagerSecret {
		m.toast = newToast("Reveal Value is only available for Secrets Manager secrets", 3*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	if lazy == nil {
		if m.lazyDetailsState[lazyDetailKey{Type: r.Type, Key: r.Key}] == lazyStateInFlight {
			m.toast = newToast("details still resolving — try again", 2*time.Second)
		} else {
			m.toast = newToast("secret value not yet resolved — try again", 2*time.Second)
		}
		return m, nil
	}
	if lazy["binary"] == "true" {
		m.toast = newErrorToast("secret is binary — view via the AWS console")
		return m, nil
	}
	if lazy[awssm.SensitiveRevealedKey] == "true" {
		delete(lazy, awssm.SensitiveRevealedKey)
		m.toast = newToast("value hidden", 2*time.Second)
	} else {
		lazy[awssm.SensitiveRevealedKey] = "true"
		m.toast = newToast("value revealed — will re-hide on refresh", 3*time.Second)
	}
	return m, nil
}

// execSecretCopyValue copies the resolved secret value to the clipboard.
// Binary-only secrets surface an informational toast instead.
func execSecretCopyValue(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSecretsManagerSecret {
		m.toast = newToast("Copy Value is only available for Secrets Manager secrets", 3*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	if lazy == nil {
		if m.lazyDetailsState[lazyDetailKey{Type: r.Type, Key: r.Key}] == lazyStateInFlight {
			m.toast = newToast("details still resolving — try again", 2*time.Second)
		} else {
			m.toast = newToast("secret value not yet resolved — try again", 2*time.Second)
		}
		return m, nil
	}
	if lazy["binary"] == "true" {
		m.toast = newErrorToast("secret is binary — copy via the AWS console")
		return m, nil
	}
	if err := copyToClipboard(lazy["value"]); err != nil {
		m.toast = newErrorToast(err.Error())
		return m, nil
	}
	m.toast = newToast("value copied", 3*time.Second)
	return m, nil
}

// execSecretUpdateValue opens $EDITOR pre-filled with the current
// SecretString. After save, secretUpdateCmd writes the new value with
// PutSecretValue.
func execSecretUpdateValue(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSecretsManagerSecret {
		m.toast = newToast("Update Value is only available for Secrets Manager secrets", 3*time.Second)
		return m, nil
	}

	lazy := m.lazyDetailsFor(r)
	if lazy != nil && lazy["binary"] == "true" {
		m.toast = newErrorToast("secret is binary — update via the AWS console")
		return m, nil
	}
	currentValue := ""
	if lazy != nil {
		currentValue = lazy["value"]
	}

	f, err := os.CreateTemp("", "scout-secret-*.txt")
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

	m.pendingEditorAction = editorActionSecretUpdate
	m.pendingEditorPath = f.Name()
	m.pendingEditorResource = m.detailsResource
	return m, openEditorCmd(f.Name())
}

// secretUpdateCmd calls secretsmanager:PutSecretValue with the new
// SecretString read from the editor temp file. On success the handler
// invalidates the lazyDetails cache via refetchDetails so the panel
// re-renders with the fresh version.
func secretUpdateCmd(ac *awsctx.Context, r core.Resource, content []byte) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := awssm.PutSecretValue(ctx, ac, r.Key, string(content)); err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("update failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "secret updated", refetchDetails: true, success: true}
	}
}
