package automation

import (
	"context"
	"fmt"
	"time"

	awsautomation "github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

func moduleActions(m *Module, r core.Row) []module.Action {
	// Execution virtual rows expose a narrower action set (no "Run").
	if isExec(r) {
		return []module.Action{
			{
				Label: "Open in Browser",
				Run: func(ctx module.Context, r core.Row) effect.Effect {
					return effect.Browser{URL: m.ConsoleURL(r, ctx.AWSCtx.Region)}
				},
			},
		}
	}
	return []module.Action{
		{
			Label: "Open in Browser",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.Browser{URL: m.ConsoleURL(r, ctx.AWSCtx.Region)}
			},
		},
		{
			Label: "Run",
			Run:   runAction,
		},
	}
}

func runAction(ctx module.Context, r core.Row) effect.Effect {
	// Fetch the document to learn parameter names, then open $EDITOR
	// pre-filled with a JSON template. OnSave parses and starts the
	// execution.
	return effect.Async{
		Label: "loading runbook params",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsautomation.DescribeDocument(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("describe failed: %v", err), Level: effect.LevelError}
			}
			prefill := buildParamTemplate(d.Parameters)
			return effect.Editor{
				Prefill: prefill,
				OnSave: func(content []byte) effect.Effect {
					params, perr := parseParamsJSON(content)
					if perr != nil {
						return effect.Toast{Message: "invalid JSON — run cancelled", Level: effect.LevelError}
					}
					return effect.Async{
						Label: "starting execution",
						Fn: func() effect.Effect {
							sc, scancel := context.WithTimeout(context.Background(), 30*time.Second)
							defer scancel()
							execID, err := awsautomation.StartExecution(sc, ctx.AWSCtx, r.Key, params)
							if err != nil {
								return effect.Toast{Message: fmt.Sprintf("start failed: %v", err), Level: effect.LevelError}
							}
							return effect.OpenVirtualDetails{
								PackageID: packageID,
								Key:       execKeyPrefix + execID,
								Name:      execID,
							}
						},
					}
				},
			}
		},
	}
}
