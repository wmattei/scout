// Package ecs is the ECS module. Ships two Module implementations
// backed by the same package: servicesModule (ECS services) and
// taskDefsModule (task definition families).
package ecs

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wmattei/scout/internal/awsctx"
	awsecs "github.com/wmattei/scout/internal/awsctx/ecs"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

const (
	servicesPackageID = "ecs"
	taskDefsPackageID = "ecs-td"
)

// NewServices returns the ECS services module.
func NewServices() module.Module { return &servicesModule{} }

// NewTaskDefs returns the ECS task-def families module.
func NewTaskDefs() module.Module { return &taskDefsModule{} }

// ---- helpers shared between the two modules ----

func readCacheBy(ctx module.Context, packageID, query string) []core.Row {
	if ctx.Cache == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	all, err := ctx.Cache.RowsByPackage(qctx, packageID)
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

func toRowsPkg(raw []core.Resource, pkg string) []core.Row {
	out := make([]core.Row, 0, len(raw))
	for _, r := range raw {
		meta := map[string]string{}
		for k, v := range r.Meta {
			meta[k] = v
		}
		// Stash the adapter Key (ARN) in Meta so generic Module.ARN()
		// still has it — but the Row Key is the display name for
		// nicer console URL building.
		meta["_key"] = r.Key
		out = append(out, core.Row{
			PackageID: pkg,
			Key:       r.DisplayName,
			Name:      r.DisplayName,
			Meta:      meta,
		})
	}
	return out
}

// ============ Services module ============

type servicesModule struct{}

var _ module.Module = (*servicesModule)(nil)

func (servicesModule) Manifest() module.Manifest {
	return module.Manifest{
		ID:      servicesPackageID,
		Name:    "ECS Services",
		Aliases: []string{"ecs", "svc", "services"},
		Tag:     "ECS",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#AF8700", Dark: "#FFD75F"}),
		SortPriority: 2,
	}
}

func (servicesModule) PollingInterval() time.Duration { return 10 * time.Second }
func (servicesModule) AlwaysRefresh() bool             { return true }

func (servicesModule) ARN(r core.Row) string { return r.Meta["_key"] }

func (servicesModule) ConsoleURL(r core.Row, region string) string {
	cluster := r.Meta[awsecs.MetaCluster]
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s/health?region=%s",
		region, url.PathEscape(cluster), url.PathEscape(r.Key), region)
}

func (m *servicesModule) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	listed := len(state.Bytes) > 0 && state.Bytes[0] == 1
	rows := readCacheBy(ctx, servicesPackageID, query)
	if listed {
		return rows, state, nil
	}
	newState := effect.State{Bytes: []byte{1}}
	effects := []effect.Effect{
		effect.Async{
			Label: "listing ecs services",
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				raw, err := awsecs.ListServices(c, ctx.AWSCtx, awsctx.ListOptions{})
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("ecs list failed: %v", err), Level: effect.LevelError}
				}
				return effect.UpsertCache{Rows: toRowsPkg(raw, servicesPackageID)}
			},
		},
	}
	return rows, newState, effects
}

func (m *servicesModule) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading service details",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsecs.DescribeService(c, ctx.AWSCtx, r.Meta[awsecs.MetaClusterArn], r.Meta["_key"])
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("describe failed: %v", err), Level: effect.LevelError}
			}
			lazy := map[string]string{
				"status":          d.Status,
				"running":         fmt.Sprintf("%d", d.RunningCount),
				"desired":         fmt.Sprintf("%d", d.DesiredCount),
				"pending":         fmt.Sprintf("%d", d.PendingCount),
				"launchType":      d.LaunchType,
				"platformVersion": d.PlatformVersion,
				"taskDefinition":  d.TaskDefinition,
				"rolloutState":    d.DeploymentRolloutState,
				"rolloutReason":   d.DeploymentRolloutStateReason,
				"eventCount":      fmt.Sprintf("%d", len(d.Events)),
			}
			for i, e := range d.Events {
				lazy[fmt.Sprintf("event_%d", i)] = e
			}
			return effect.SetLazy{PackageID: servicesPackageID, Key: r.Key, Lazy: lazy}
		},
	}
}

func (servicesModule) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{Metadata: widget.Raw{Content: "resolving…"}}
	}
	var events []widget.EventRow
	var n int
	fmt.Sscanf(lazy["eventCount"], "%d", &n)
	for i := 0; i < n; i++ {
		text := lazy[fmt.Sprintf("event_%d", i)]
		if text != "" {
			events = append(events, widget.EventRow{Text: text})
		}
	}
	return module.DetailZones{
		Status: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Status", Value: lazy["status"]},
				{Label: "Running / desired", Value: lazy["running"] + " / " + lazy["desired"]},
				{Label: "Pending", Value: lazy["pending"]},
				{Label: "Rollout", Value: lazy["rolloutState"]},
			},
		},
		Metadata: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Launch type", Value: lazy["launchType"]},
				{Label: "Platform", Value: lazy["platformVersion"]},
				{Label: "Task definition", Value: lazy["taskDefinition"]},
			},
		},
		Events: widget.EventList{Rows: events},
	}
}

func (m *servicesModule) Actions(r core.Row) []module.Action {
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
			Label: "Force Deploy",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				return effect.Confirm{
					Prompt: "Force deploy " + r.Name + "?",
					OnYes: effect.Async{
						Label: "force deploy",
						Fn: func() effect.Effect {
							c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
							defer cancel()
							if err := awsecs.ForceDeployment(c, ctx.AWSCtx, r.Meta[awsecs.MetaClusterArn], r.Meta["_key"]); err != nil {
								return effect.Toast{Message: fmt.Sprintf("force deploy failed: %v", err), Level: effect.LevelError}
							}
							return effect.Toast{Message: "force deploy triggered", Level: effect.LevelSuccess}
						},
					},
				}
			},
		},
		{
			Label: "Tail Logs",
			Run: func(ctx module.Context, r core.Row) effect.Effect {
				family := r.Meta[awsecs.MetaTaskDefFamily]
				if family == "" {
					return effect.Toast{Message: "no log group available (unknown task def family)", Level: effect.LevelError}
				}
				return effect.TailLogs{LogGroup: "/ecs/" + family}
			},
		},
	}
}

func (servicesModule) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	return effect.None{}
}
