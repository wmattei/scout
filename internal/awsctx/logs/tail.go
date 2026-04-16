// Package logs wraps CloudWatch Logs Live Tail so the TUI can stream
// events through a plain Go channel without touching smithy event-stream
// types directly.
package logs

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwl "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/wmattei/scout/internal/awsctx"
)

// TailEvent is a single log line surfaced to the TUI. Timestamp is
// milliseconds since epoch (what CloudWatch gives us).
type TailEvent struct {
	Timestamp int64
	Message   string
}

// TailStream wraps an in-flight StartLiveTail call. Events() returns a
// receive-only channel that is closed when the stream terminates. Close()
// cancels the underlying context and drains the channel.
type TailStream struct {
	Events <-chan TailEvent
	Err    <-chan error
	cancel context.CancelFunc
}

// Close stops the stream. Safe to call multiple times.
func (s *TailStream) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// StartLiveTail resolves the log group ARN for the given account+region
// and starts a live-tail stream. The returned TailStream pipes events as
// they arrive; close it when the caller is done.
func StartLiveTail(parentCtx context.Context, ac *awsctx.Context, logGroupName, account string) (*TailStream, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	client := cwl.NewFromConfig(ac.Cfg)
	arn := fmt.Sprintf("arn:aws:logs:%s:%s:log-group:%s", ac.Region, account, logGroupName)

	out, err := client.StartLiveTail(ctx, &cwl.StartLiveTailInput{
		LogGroupIdentifiers: []string{arn},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cloudwatchlogs:StartLiveTail (group=%s): %w", logGroupName, err)
	}

	evCh := make(chan TailEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(evCh)
		defer close(errCh)
		stream := out.GetStream()
		defer stream.Close()

		for ev := range stream.Events() {
			switch e := ev.(type) {
			case *cwltypes.StartLiveTailResponseStreamMemberSessionUpdate:
				for _, r := range e.Value.SessionResults {
					msg := ""
					if r.Message != nil {
						msg = *r.Message
					}
					ts := int64(0)
					if r.Timestamp != nil {
						ts = *r.Timestamp
					}
					select {
					case evCh <- TailEvent{Timestamp: ts, Message: msg}:
					case <-ctx.Done():
						return
					}
				}
			case *cwltypes.StartLiveTailResponseStreamMemberSessionStart:
				// Session metadata; ignore for v0.
			}
		}
		if err := stream.Err(); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	return &TailStream{
		Events: evCh,
		Err:    errCh,
		cancel: cancel,
	}, nil
}

// GetRecentEvents fetches the most recent `limit` log events from the
// given log group, newest-last (chronological order). Used to
// pre-populate the tail viewport with historical context before the
// live-tail stream starts producing events.
//
// The call uses FilterLogEvents with no filter expression and a
// startTime of now minus `lookback` to avoid scanning the entire
// log group history. If the group doesn't exist or has no events in
// the window, the returned slice is empty (no error).
func GetRecentEvents(ctx context.Context, ac *awsctx.Context, logGroupName string, limit int, lookback time.Duration) ([]TailEvent, error) {
	client := cwl.NewFromConfig(ac.Cfg)

	startTime := time.Now().Add(-lookback).UnixMilli()
	input := &cwl.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		StartTime:    aws.Int64(startTime),
		Limit:        aws.Int32(int32(limit)),
		Interleaved:  aws.Bool(true),
	}

	out, err := client.FilterLogEvents(ctx, input)
	if err != nil {
		// Best-effort: log group might not exist yet, or caller might
		// not have filter permission. Return empty, not an error.
		return nil, nil
	}

	events := make([]TailEvent, 0, len(out.Events))
	for _, ev := range out.Events {
		msg := ""
		if ev.Message != nil {
			msg = *ev.Message
		}
		ts := int64(0)
		if ev.Timestamp != nil {
			ts = *ev.Timestamp
		}
		events = append(events, TailEvent{Timestamp: ts, Message: msg})
	}
	return events, nil
}

// ensure aws package is referenced so goimports doesn't drop it when
// someone edits this file in the future — the ARN construction uses
// plain string formatting but we may switch to aws.String helpers.
var _ = aws.String
