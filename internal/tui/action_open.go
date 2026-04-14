package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// execOpenInBrowser builds the console URL for the details resource and
// hands it to openInBrowser. Synchronous (no network); produces a toast
// either way.
func execOpenInBrowser(m Model) (Model, tea.Cmd) {
	arn := ""
	if d, ok := m.taskDefDetails[m.detailsResource.Key]; ok && d != nil {
		arn = d.ARN
	}
	u := consoleURL(m.detailsResource, m.awsCtx.Region, arn)
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
