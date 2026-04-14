package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execCopyURI copies a resource URI to the clipboard. Only S3 resources
// have URIs (s3://bucket/key). Other types show an informational toast.
func execCopyURI(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	var uri string
	switch r.Type {
	case core.RTypeBucket:
		uri = fmt.Sprintf("s3://%s", r.Key)
	case core.RTypeFolder, core.RTypeObject:
		uri = fmt.Sprintf("s3://%s/%s", r.Meta["bucket"], r.Key)
	default:
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

// execCopyARN copies the resource ARN to the clipboard. ARNs come from
// Resource.ARN(), which handles every type in the current set. Task-def
// families rely on lazy resolution — if taskDefDetails has the revision
// ARN, use that; otherwise fall back to the family-only pseudo-ARN.
func execCopyARN(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	arn := r.ARN()
	if r.Type == core.RTypeEcsTaskDefFamily {
		if d, ok := m.taskDefDetails[r.Key]; ok && d != nil && d.ARN != "" {
			arn = d.ARN
		}
	}
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
