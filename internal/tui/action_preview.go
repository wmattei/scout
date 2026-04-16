package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/format"

	"github.com/wmattei/scout/internal/awsctx"
	awss3 "github.com/wmattei/scout/internal/awsctx/s3"
	"github.com/wmattei/scout/internal/core"
)

// previewSizeLimit caps how large an object may be before Preview refuses
// to fetch it. 100 MB matches the spec's hard limit.
const previewSizeLimit = 100 * 1024 * 1024

// execPreview fetches an object to a temp file and opens it with the OS
// default viewer. Rejects unsupported extensions and oversized objects
// via toast.
func execPreview(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeObject {
		m.toast = newToast("preview is only available for S3 objects", 3*time.Second)
		return m, nil
	}
	if !previewAllowed(r.Key) {
		m.toast = newToast("unsupported preview format (jpg, png, txt, csv only)", 4*time.Second)
		return m, nil
	}
	bucket := r.Meta["bucket"]
	if bucket == "" {
		m.toast = newToast("object missing bucket metadata", 3*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "preparing preview…"
	m.toast = newToast("preparing preview…", 10*time.Second)
	return m, previewCmd(m.awsCtx, bucket, r.Key)
}

// previewCmd head-checks the size, streams the object into a temp file,
// and hands it to the OS viewer. Returns msgActionDone for all paths.
func previewCmd(ac *awsctx.Context, bucket, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		size, err := awss3.HeadObject(ctx, ac, bucket, key)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview head failed: %v", err),
				err:   err,
			}
		}
		if size > previewSizeLimit {
			return msgActionDone{
				toast: fmt.Sprintf("object too large for preview (%s > 100 MB)", format.Bytes(fmt.Sprintf("%d", size))),
				err:   fmt.Errorf("size %d over limit", size),
			}
		}

		path, err := previewTempPath(key)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview temp path: %v", err),
				err:   err,
			}
		}
		f, err := os.Create(path)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("create temp file: %v", err),
				err:   err,
			}
		}
		_, err = awss3.StreamObject(ctx, ac, bucket, key, f)
		_ = f.Close()
		if err != nil {
			_ = os.Remove(path)
			return msgActionDone{
				toast: fmt.Sprintf("preview download failed: %v", err),
				err:   err,
			}
		}
		if err := openPreview(path); err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("preview open failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "preview opened", err: nil}
	}
}
