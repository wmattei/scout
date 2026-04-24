package ssm

import (
	"context"
	"fmt"
	"time"

	awsssm "github.com/wmattei/scout/internal/awsctx/ssm"
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
				if arn := m.ARN(r); arn != "" {
					return effect.Copy{Text: arn, Label: "ARN"}
				}
				return fetchAndThen(ctx, r, func(d *awsssm.ParameterDetails) effect.Effect {
					return effect.Copy{Text: d.ARN, Label: "ARN"}
				})
			},
		},
		{
			Label: "Copy Value",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return fetchAndThen(ctx, r, func(d *awsssm.ParameterDetails) effect.Effect {
					return effect.Copy{Text: d.Value, Label: "value"}
				})
			},
		},
		{
			Label: "Update Value",
			Run:   updateAction,
		},
	}
}

// fetchAndThen runs GetParameter and passes the result into next.
// Used by actions that need the current value/ARN but may be invoked
// before Details has resolved.
func fetchAndThen(ctx module.Context, r core.Row, next func(*awsssm.ParameterDetails) effect.Effect) effect.Effect {
	return effect.Async{
		Label: "fetching parameter",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsssm.GetParameter(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("get parameter failed: %v", err), Level: effect.LevelError}
			}
			return next(d)
		},
	}
}

// updateAction fetches the current value, opens $EDITOR pre-filled
// with it, and PutParameter on save preserving the parameter type.
func updateAction(ctx module.Context, r core.Row) effect.Effect {
	return fetchAndThen(ctx, r, func(d *awsssm.ParameterDetails) effect.Effect {
		paramType := d.Type
		return effect.Editor{
			Prefill: []byte(d.Value),
			OnSave: func(content []byte) effect.Effect {
				return effect.Async{
					Label: "updating parameter",
					Fn: func() effect.Effect {
						c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						if err := awsssm.PutParameter(c, ctx.AWSCtx, r.Key, string(content), paramType); err != nil {
							return effect.Toast{Message: fmt.Sprintf("put parameter failed: %v", err), Level: effect.LevelError}
						}
						return effect.Toast{Message: "parameter updated", Level: effect.LevelSuccess}
					},
				}
			},
		}
	})
}
