package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	awsautomation "github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/effect"
	"github.com/wmattei/scout/internal/module"
	"github.com/wmattei/scout/internal/widget"
)

// pollInterval is the cadence between GetExecution refetches for a
// non-terminal execution. Matches the legacy TUI's cadence so AWS
// rate limits stay comfortable.
const pollInterval = 3 * time.Second

func resolveExecution(ctx module.Context, r core.Row) effect.Effect {
	return effect.Async{
		Label: "loading execution",
		Fn:    fetchExecutionFn(ctx, r),
	}
}

// fetchExecutionFn closes over the module Context and the execution
// row. Returns a Batch of SetLazy + (optionally) Tick re-poll.
func fetchExecutionFn(ctx module.Context, r core.Row) func() effect.Effect {
	return func() effect.Effect {
		execID := strings.TrimPrefix(r.Key, execKeyPrefix)
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := awsautomation.GetExecution(c, ctx.AWSCtx, execID)
		if err != nil {
			return effect.Toast{Message: fmt.Sprintf("get execution failed: %v", err), Level: effect.LevelError}
		}
		lazy := executionLazy(d)
		setLazy := effect.SetLazy{PackageID: packageID, Key: r.Key, Lazy: lazy}
		if awsautomation.IsTerminalStatus(d.Status) {
			return setLazy
		}
		// Still running — re-poll after pollInterval.
		return effect.Batch{Effects: []effect.Effect{
			setLazy,
			effect.Tick{
				After: pollInterval,
				Then: effect.Async{
					Label: "polling execution",
					Fn:    fetchExecutionFn(ctx, r),
				},
			},
		}}
	}
}

func executionLazy(d *awsautomation.ExecutionDetails) map[string]string {
	lazy := map[string]string{
		"execId":       d.ExecutionID,
		"document":     d.DocumentName,
		"version":      d.DocumentVersion,
		"status":       d.Status,
		"mode":         d.Mode,
		"executedBy":   d.ExecutedBy,
		"failure":      d.FailureMessage,
		"startTime":    d.StartTime.Format(time.RFC3339),
		"endTime":      d.EndTime.Format(time.RFC3339),
		"stepCount":    fmt.Sprintf("%d", len(d.Steps)),
	}
	for i, s := range d.Steps {
		prefix := fmt.Sprintf("step_%d_", i)
		lazy[prefix+"name"] = s.StepName
		lazy[prefix+"action"] = s.Action
		lazy[prefix+"status"] = s.Status
		lazy[prefix+"duration"] = s.Duration().Truncate(time.Second).String()
		if s.FailureMessage != "" {
			lazy[prefix+"failure"] = s.FailureMessage
		}
	}
	return lazy
}

func buildExecution(r core.Row, lazy map[string]string) module.DetailZones {
	if lazy == nil {
		return module.DetailZones{
			Metadata: widget.Raw{Content: "resolving…"},
		}
	}

	status := lazy["status"]
	pill := widget.StatusPill{
		Text:  status,
		Level: statusLevel(status),
	}

	var stepRows []widget.EventRow
	var n int
	fmt.Sscanf(lazy["stepCount"], "%d", &n)
	for i := 0; i < n; i++ {
		prefix := fmt.Sprintf("step_%d_", i)
		name := lazy[prefix+"name"]
		if name == "" {
			continue
		}
		text := fmt.Sprintf("%-20s %-14s %8s  %s",
			trunc(name, 20),
			lazy[prefix+"status"],
			lazy[prefix+"duration"],
			lazy[prefix+"action"],
		)
		stepRows = append(stepRows, widget.EventRow{Text: text})
	}

	metadata := widget.KeyValue{
		Rows: []widget.KVRow{
			{Label: "Document", Value: lazy["document"]},
			{Label: "Version", Value: lazy["version"]},
			{Label: "Mode", Value: lazy["mode"]},
			{Label: "Executed by", Value: lazy["executedBy"]},
			{Label: "Started", Value: lazy["startTime"]},
			{Label: "Ended", Value: lazy["endTime"]},
		},
	}
	if fm := lazy["failure"]; fm != "" {
		metadata.Rows = append(metadata.Rows, widget.KVRow{Label: "Failure", Value: fm})
	}

	return module.DetailZones{
		Status:   pill,
		Metadata: metadata,
		Events:   widget.EventList{Rows: stepRows},
	}
}

func statusLevel(status string) effect.Level {
	switch status {
	case "Success", "CompletedWithSuccess":
		return effect.LevelSuccess
	case "Failed", "TimedOut", "CompletedWithFailure", "Cancelled", "Rejected":
		return effect.LevelError
	case "InProgress", "Pending", "Waiting":
		return effect.LevelWarning
	}
	return effect.LevelInfo
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
