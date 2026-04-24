package ssm

import (
	"context"
	"fmt"
	"strconv"
	"time"

	awsssm "github.com/wmattei/scout/internal/awsctx/ssm"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

func resolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading parameter details",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsssm.GetParameter(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("get parameter failed: %v", err), Level: effect.LevelError}
			}
			lazy := map[string]string{
				"arn":          d.ARN,
				"type":         d.Type,
				"value":        d.Value,
				"version":      strconv.FormatInt(d.Version, 10),
				"dataType":     d.DataType,
				"lastModified": d.LastModified.Format(time.RFC3339),
			}
			return effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
		},
	}
}

func buildDetails(r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{
			Metadata: widget.Raw{Content: "resolving…"},
		}
	}
	return module.DetailZones{
		Status: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Type", Value: lazy["type"]},
				{Label: "Version", Value: lazy["version"]},
				{Label: "Data type", Value: lazy["dataType"]},
			},
		},
		Metadata: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Modified", Value: lazy["lastModified"]},
			},
		},
		Value: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Value", Value: lazy["value"], Clickable: true, ClipValue: lazy["value"]},
			},
		},
	}
}
