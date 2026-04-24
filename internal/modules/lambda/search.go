package lambda

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wmattei/scout/internal/awsctx"
	awslambda "github.com/wmattei/scout/internal/awsctx/lambda"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

// handleSearch serves cached rows on every keystroke and fires a
// live ListFunctions via Async on first entry into the module (when
// the state's "listed" flag is false).
func handleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	listed := len(state.Bytes) > 0 && state.Bytes[0] == 1

	rows := readCache(ctx, query)

	if listed {
		return rows, state, nil
	}

	newState := effect.State{Bytes: []byte{1}}
	effects := []effect.Effect{
		effect.Async{
			Label: "listing lambda functions",
			Fn:    listAllFn(ctx),
		},
	}
	return rows, newState, effects
}

func readCache(ctx module.Context, query string) []core.Row {
	if ctx.Cache == nil {
		return nil
	}
	queryCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	all, err := ctx.Cache.RowsByPackage(queryCtx, packageID)
	if err != nil {
		return nil
	}
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	out := make([]core.Row, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}

func listAllFn(ctx module.Context) func() effect.Effect {
	return func() effect.Effect {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		raw, err := awslambda.ListFunctions(c, ctx.AWSCtx, awsctx.ListOptions{})
		if err != nil {
			return effect.Toast{Message: fmt.Sprintf("lambda list failed: %v", err), Level: effect.LevelError}
		}
		return effect.UpsertCache{Rows: toRows(raw)}
	}
}

// toRows maps the adapter's []core.Resource records onto []core.Row.
// Key = function name (so ConsoleURL works); ARN is carried in Meta.
func toRows(raw []core.Resource) []core.Row {
	out := make([]core.Row, 0, len(raw))
	for _, r := range raw {
		meta := map[string]string{"arn": r.Key}
		for k, v := range r.Meta {
			meta[k] = v
		}
		out = append(out, core.Row{
			PackageID: packageID,
			Key:       r.DisplayName,
			Name:      r.DisplayName,
			Meta:      meta,
		})
	}
	return out
}
