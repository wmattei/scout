package lambda

import (
	"context"
	"fmt"
	"strconv"
	"time"

	awslambda "github.com/wmattei/scout/internal/awsctx/lambda"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

func resolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading lambda details",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awslambda.GetFunction(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("describe failed: %v", err), Level: effect.LevelError}
			}
			lazy := map[string]string{
				"runtime":      d.Runtime,
				"memorySize":   strconv.FormatInt(int64(d.MemorySize), 10),
				"timeout":      strconv.FormatInt(int64(d.Timeout), 10),
				"handler":      d.Handler,
				"codeSize":     strconv.FormatInt(d.CodeSize, 10),
				"lastModified": d.LastModified,
				"description":  d.Description,
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
				{Label: "Runtime", Value: lazy["runtime"]},
				{Label: "Memory", Value: lazy["memorySize"] + " MB"},
				{Label: "Timeout", Value: lazy["timeout"] + "s"},
			},
		},
		Metadata: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Handler", Value: lazy["handler"]},
				{Label: "Code size", Value: lazy["codeSize"] + " bytes"},
				{Label: "Modified", Value: lazy["lastModified"]},
				{Label: "Description", Value: lazy["description"]},
			},
		},
	}
}
