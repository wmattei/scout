package secrets

import (
	"context"
	"fmt"
	"time"

	awssm "github.com/wmattei/scout/internal/awsctx/secretsmanager"
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
				return fetchAndThen(ctx, r, func(d *awssm.SecretDetails) effect.Effect {
					return effect.Copy{Text: d.ARN, Label: "ARN"}
				})
			},
		},
		{
			Label: "Reveal Value",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.SetState{
					PackageID: packageID,
					State:     toggleReveal(ctx.State),
				}
			},
		},
		{
			Label: "Copy Value",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return fetchAndThen(ctx, r, func(d *awssm.SecretDetails) effect.Effect {
					return effect.Copy{Text: d.Value, Label: "secret value"}
				})
			},
		},
		{
			Label: "Update Value",
			Run:   updateAction,
		},
	}
}

func fetchAndThen(ctx module.Context, r core.Row, next func(*awssm.SecretDetails) effect.Effect) effect.Effect {
	return effect.Async{
		Label: "fetching secret",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awssm.GetSecretValue(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("get secret failed: %v", err), Level: effect.LevelError}
			}
			return next(d)
		},
	}
}

func updateAction(ctx module.Context, r core.Row) effect.Effect {
	return fetchAndThen(ctx, r, func(d *awssm.SecretDetails) effect.Effect {
		return effect.Editor{
			Prefill: []byte(d.Value),
			OnSave: func(content []byte) effect.Effect {
				return effect.Async{
					Label: "updating secret",
					Fn: func() effect.Effect {
						c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						if err := awssm.PutSecretValue(c, ctx.AWSCtx, r.Key, string(content)); err != nil {
							return effect.Toast{Message: fmt.Sprintf("put secret failed: %v", err), Level: effect.LevelError}
						}
						return effect.Toast{Message: "secret updated", Level: effect.LevelSuccess}
					},
				}
			},
		}
	})
}
