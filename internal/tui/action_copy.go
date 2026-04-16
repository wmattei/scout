package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/services"
)

// execCopyURI copies a resource URI to the clipboard. Only S3 resources
// have URIs (s3://bucket/key). Other types show an informational toast.
func execCopyURI(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	p, ok := services.Get(r.Type)
	if !ok {
		m.toast = newToast("no URI for this resource type", 3*time.Second)
		return m, nil
	}
	uri, supported := p.URI(r)
	if !supported {
		m.toast = newToast("no URI for this resource type", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(uri); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("URI copied: "+uri, 3*time.Second)
	return m, nil
}

// execCopyARN copies the resource ARN to the clipboard. Goes through
// arnForDetails so the lazy-resolved revision ARN is preferred over the
// family pseudo-ARN when available.
func execCopyARN(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	arn := arnForDetails(r, m)
	if arn == "" {
		m.toast = newToast("no ARN for this resource", 3*time.Second)
		return m, nil
	}
	if err := copyToClipboard(arn); err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}
	m.toast = newToast("ARN copied: "+arn, 3*time.Second)
	return m, nil
}

// arnForDetails returns the best ARN we can show for the given
// resource. Delegates to the provider's ARN method (which itself reads
// from the lazy map for types like ECS task-def families that need
// the resolved revision) and falls back to core.Resource.ARN() when
// no provider is registered.
func arnForDetails(r core.Resource, m Model) string {
	lazy := m.lazyDetailsFor(r)
	if p, ok := services.Get(r.Type); ok {
		if a := p.ARN(r, lazy); a != "" {
			return a
		}
	}
	return ""
}
