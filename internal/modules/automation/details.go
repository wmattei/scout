package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	awsautomation "github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

// resolveDocument fetches document metadata + the last 10 executions.
// Packs both into a single lazy map so BuildDetails can render the
// Events zone from the same payload.
func resolveDocument(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading runbook",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsautomation.DescribeDocument(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("describe runbook failed: %v", err), Level: effect.LevelError}
			}
			execs, _ := awsautomation.ListExecutions(c, ctx.AWSCtx, r.Key, 10)

			lazy := map[string]string{
				"docType":        d.DocumentType,
				"targetType":     d.TargetType,
				"owner":          d.Owner,
				"latestVersion": d.LatestVersion,
				"versionName":   d.VersionName,
				"platformTypes": strings.Join(d.PlatformTypes, ", "),
			}
			// Encode the executions list into the lazy map as "n" fields
			// so BuildDetails can reconstruct the EventList.
			lazy["execCount"] = fmt.Sprintf("%d", len(execs))
			for i, e := range execs {
				prefix := fmt.Sprintf("exec_%d_", i)
				lazy[prefix+"id"] = e.ExecutionID
				lazy[prefix+"status"] = e.Status
				lazy[prefix+"start"] = e.StartTime.Format(time.RFC3339)
			}
			return effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
		},
	}
}

func buildDocument(r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{
			Metadata: widget.Raw{Content: "resolving…"},
		}
	}
	var events []widget.EventRow
	if n := parseExecCount(lazy); n > 0 {
		events = make([]widget.EventRow, 0, n)
		for i := 0; i < n; i++ {
			prefix := fmt.Sprintf("exec_%d_", i)
			id := lazy[prefix+"id"]
			if id == "" {
				continue
			}
			text := fmt.Sprintf("%s  %s  %s", shortID(id), lazy[prefix+"status"], lazy[prefix+"start"])
			events = append(events, widget.EventRow{Text: text, ActivationID: id})
		}
	}
	return module.DetailZones{
		Status: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Type", Value: lazy["docType"]},
				{Label: "Target", Value: lazy["targetType"]},
				{Label: "Platforms", Value: lazy["platformTypes"]},
			},
		},
		Metadata: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Owner", Value: lazy["owner"]},
				{Label: "Latest version", Value: lazy["latestVersion"]},
				{Label: "Version name", Value: lazy["versionName"]},
			},
		},
		Events: widget.EventList{
			Rows:       events,
			Selectable: true,
		},
	}
}

func parseExecCount(lazy map[string]string) int {
	var n int
	fmt.Sscanf(lazy["execCount"], "%d", &n)
	return n
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8] + "…"
	}
	return id
}

// BuildDetails entry for the document path requires r only in case
// the module grows per-row rendering decisions.
var _ = core.Row{}
