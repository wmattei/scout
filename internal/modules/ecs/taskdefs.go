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

type taskDefsModule struct{}

var _ module.Module = (*taskDefsModule)(nil)

func (taskDefsModule) Manifest() module.Manifest {
	return module.Manifest{
		ID:      taskDefsPackageID,
		Name:    "ECS Task Definitions",
		Aliases: []string{"td", "task", "taskdef"},
		Tag:     "TD ",
		TagStyle: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFAF5F"}),
		SortPriority: 7,
	}
}

func (taskDefsModule) PollingInterval() time.Duration { return 0 }
func (taskDefsModule) AlwaysRefresh() bool             { return true }

func (taskDefsModule) ARN(r core.Row) string { return r.Meta["arn"] }

func (taskDefsModule) ConsoleURL(r core.Row, region string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions/%s?region=%s",
		region, url.PathEscape(r.Key), region)
}

func (m *taskDefsModule) HandleSearch(ctx module.Context, query string, state effect.State) ([]core.Row, effect.State, []effect.Effect) {
	listed := len(state.Bytes) > 0 && state.Bytes[0] == 1
	rows := readCacheBy(ctx, taskDefsPackageID, query)
	if listed {
		return rows, state, nil
	}
	newState := effect.State{Bytes: []byte{1}}
	effects := []effect.Effect{
		effect.Async{
			Label: "listing task defs",
			Fn: func() effect.Effect {
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				raw, err := awsecs.ListTaskDefFamilies(c, ctx.AWSCtx, awsctx.ListOptions{})
				if err != nil {
					return effect.Toast{Message: fmt.Sprintf("task def list failed: %v", err), Level: effect.LevelError}
				}
				return effect.UpsertCache{Rows: toRowsPkg(raw, taskDefsPackageID)}
			},
		},
	}
	return rows, newState, effects
}

func (m *taskDefsModule) ResolveDetails(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading task definition",
		Fn: func() effect.Effect {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			d, err := awsecs.DescribeFamily(c, ctx.AWSCtx, r.Key)
			if err != nil {
				return effect.Toast{Message: fmt.Sprintf("describe failed: %v", err), Level: effect.LevelError}
			}
			lazy := map[string]string{
				"arn":             d.ARN,
				"revision":        fmt.Sprintf("%d", d.Revision),
				"cpu":             d.CPU,
				"memory":          d.Memory,
				"networkMode":     d.NetworkMode,
				"taskRoleArn":     d.TaskRoleArn,
				"executionRole":   d.ExecutionRoleArn,
				"compatibilities": strings.Join(d.RequiresCompatibilities, ", "),
				"containers":      strings.Join(d.ContainerImages, "\n"),
			}
			return effect.SetLazy{PackageID: taskDefsPackageID, Key: r.Key, Lazy: lazy}
		},
	}
}

func (taskDefsModule) BuildDetails(ctx module.Context, r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{Metadata: widget.Raw{Content: "resolving…"}}
	}
	return module.DetailZones{
		Status: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Revision", Value: lazy["revision"]},
				{Label: "CPU / memory", Value: lazy["cpu"] + " / " + lazy["memory"]},
				{Label: "Network mode", Value: lazy["networkMode"]},
			},
		},
		Metadata: widget.KeyValue{
			Rows: []widget.KVRow{
				{Label: "Compatibilities", Value: lazy["compatibilities"]},
				{Label: "Task role", Value: lazy["taskRoleArn"]},
				{Label: "Execution role", Value: lazy["executionRole"]},
			},
		},
		Value: widget.Raw{Content: lazy["containers"]},
	}
}

func (m *taskDefsModule) Actions(r core.Row) []module.Action {
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
				return effect.Toast{Message: "ARN available after opening Details", Level: effect.LevelWarning}
			},
		},
	}
}

func (taskDefsModule) HandleEvent(ctx module.Context, r core.Row, activationID string) effect.Effect {
	return effect.None{}
}
