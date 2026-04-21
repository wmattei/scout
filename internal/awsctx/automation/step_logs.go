package automation

import (
	"context"
	"fmt"
	"time"

	"github.com/wmattei/scout/internal/awsctx"
	awslogs "github.com/wmattei/scout/internal/awsctx/logs"
)

// StepLogGroup returns the CloudWatch log group scout should tail or
// snapshot for a given step. Currently only Lambda steps are
// resolved; non-Lambda steps return ("", false) and the renderer
// falls back to showing Inputs/Outputs/FailureMessage.
func StepLogGroup(s StepDetails) (string, bool) {
	if name, ok := s.LambdaFunctionName(); ok && name != "" {
		return "/aws/lambda/" + name, true
	}
	return "", false
}

// StepLogSnapshot fetches the recent events for a step's associated
// log group over the time window the step was active. Falls back to
// the last 30 minutes when the step has no explicit start/end (e.g.
// still pending). Returns (nil, nil) for steps with no log group.
func StepLogSnapshot(ctx context.Context, ac *awsctx.Context, s StepDetails, limit int) ([]string, error) {
	group, ok := StepLogGroup(s)
	if !ok {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	lookback := 30 * time.Minute
	if !s.StartTime.IsZero() {
		lookback = time.Since(s.StartTime) + 30*time.Second
		if lookback < time.Minute {
			lookback = time.Minute
		}
	}

	events, err := awslogs.GetRecentEvents(ctx, ac, group, limit, lookback)
	if err != nil {
		return nil, fmt.Errorf("fetch step logs: %w", err)
	}

	lines := make([]string, 0, len(events))
	for _, e := range events {
		lines = append(lines, e.Message)
	}
	return lines, nil
}
