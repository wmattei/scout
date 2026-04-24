package secrets

import (
	"context"
	"fmt"
	"strings"
	"time"

	awssm "github.com/wmattei/scout/internal/awsctx/secretsmanager"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

func resolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading secret",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awssm.GetSecretValue(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("get secret failed: %v", err), Level: effect.LevelError}
			}
			lazy := map[string]string{
				"arn":         d.ARN,
				"value":       d.Value,
				"versionId":   d.VersionID,
				"createdDate": d.CreatedDate.Format(time.RFC3339),
			}
			return effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
		},
	}
}

func buildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{
			Metadata: widget.Raw{Content: "resolving…"},
		}
	}
	raw := lazy["value"]
	showReveal := revealed(ctx.State)
	display := raw
	if !showReveal {
		display = strings.Repeat("•", 12) + "  <hidden — use Reveal Value>"
	}
	return module.DetailZones{
		Status: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Version", Value: lazy["versionId"]},
				{Label: "Created", Value: lazy["createdDate"]},
			},
		},
		Value: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Value", Value: display, Clickable: true, ClipValue: raw},
			},
		},
	}
}
