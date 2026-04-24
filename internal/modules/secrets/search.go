package secrets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wmattei/scout/internal/awsctx"
	awssm "github.com/wmattei/scout/internal/awsctx/secretsmanager"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
)

// Reserve the high byte(s) of state.Bytes for non-reveal flags so
// future additions don't collide with bit 0.
const stateListedBit = 1 << 1

func handleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	rows := readCache(ctx, query)
	if listed(state) {
		return rows, state, nil
	}
	newState := setListed(state)
	effects := []effect.Effect{
		effect.Async{
			Label: "listing secrets",
			Fn:    listAllFn(ctx),
		},
	}
	return rows, newState, effects
}

func listed(s effect.State) bool {
	return len(s.Bytes) > 0 && s.Bytes[0]&stateListedBit != 0
}

func setListed(s effect.State) effect.State {
	var b byte
	if len(s.Bytes) > 0 {
		b = s.Bytes[0]
	}
	return effect.State{Bytes: []byte{b | stateListedBit}}
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
		raw, err := awssm.ListSecrets(c, ctx.AWSCtx, awsctx.ListOptions{})
		if err != nil {
			return effect.Toast{Message: fmt.Sprintf("secrets list failed: %v", err), Level: effect.LevelError}
		}
		return effect.UpsertCache{Rows: toRows(raw)}
	}
}

func toRows(raw []core.Resource) []core.Row {
	out := make([]core.Row, 0, len(raw))
	for _, r := range raw {
		meta := map[string]string{}
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
