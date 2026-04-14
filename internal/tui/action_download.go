package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execDownload streams the selected S3 object into the user's downloads
// directory and produces a toast on completion.
func execDownload(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeObject {
		m.toast = newToast("download is only available for S3 objects", 3*time.Second)
		return m, nil
	}
	bucket := r.Meta["bucket"]
	if bucket == "" {
		m.toast = newToast("object missing bucket metadata", 3*time.Second)
		return m, nil
	}

	basename := filepath.Base(r.Key)
	dest, err := downloadPathFor(basename)
	if err != nil {
		m.toast = newToast(err.Error(), 4*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "downloading…"
	m.toast = newToast(fmt.Sprintf("downloading to %s…", dest), 10*time.Second)
	return m, downloadCmd(m.awsCtx, bucket, r.Key, dest)
}

// downloadCmd streams an object to disk and emits msgActionDone.
func downloadCmd(ac *awsctx.Context, bucket, key, dest string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		f, err := os.Create(dest)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("create file failed: %v", err),
				err:   err,
			}
		}
		defer f.Close()

		n, err := awss3.StreamObject(ctx, ac, bucket, key, f)
		if err != nil {
			_ = os.Remove(dest)
			return msgActionDone{
				toast: fmt.Sprintf("download failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{
			toast: fmt.Sprintf("downloaded %s (%s)", dest, formatBytes(fmt.Sprintf("%d", n))),
			err:   nil,
		}
	}
}
