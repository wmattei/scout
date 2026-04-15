package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/services"
)

// execOpenInBrowser builds the console URL for the details resource and
// hands it to openInBrowser. Synchronous (no network); produces a toast
// either way.
func execOpenInBrowser(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no console URL for this resource", 3*time.Second)
		return m, nil
	}
	lazy := m.lazyDetailsFor(r)
	u := p.ConsoleURL(r, m.awsCtx.Region, lazy)
	if u == "" {
		m.toast = newToast("no console URL for this resource", 3*time.Second)
		return m, nil
	}
	if err := openInBrowser(u); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("opened in browser", 2*time.Second)
	return m, nil
}
