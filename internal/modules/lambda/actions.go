package lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	awslambda "github.com/wmattei/scout/internal/awsctx/lambda"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

func moduleActions(m *Module) []module.Action {
	return []module.Action{
		{
			Label: "Open in Browser",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.Browser{URL: m.ConsoleURL(r, ctx.AWSCtx.Region)}
			},
		},
		{
			Label: "Copy ARN",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.Copy{Text: m.ARN(r), Label: "ARN"}
			},
		},
		{
			Label: "Run",
			Run:   invokeAction,
		},
		{
			Label: "Tail Logs",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.TailLogs{LogGroup: "/aws/lambda/" + r.Key}
			},
		},
	}
}

func invokeAction(ctx module.Context, r core.Row) effect.Effect {
	prefill := []byte("{}\n")
	return effect.Editor{
		Prefill: prefill,
		OnSave: func(content []byte) effect.Effect {
			if !json.Valid(content) {
				return effect.Toast{Message: "invalid JSON payload — invoke cancelled", Level: effect.LevelError}
			}
			return effect.Async{
				Label: "invoking…",
				Fn: func() effect.Effect {
					c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					result, err := awslambda.InvokeFunction(c, ctx.AWSCtx, r.Key, content)
					if err != nil {
						return effect.Toast{Message: fmt.Sprintf("invoke failed: %v", err), Level: effect.LevelError}
					}
					return effect.Toast{
						Message: fmt.Sprintf("invoked: status=%d", result.StatusCode),
						Level:   effect.LevelSuccess,
					}
				},
			}
		},
	}
}
