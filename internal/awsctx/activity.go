package awsctx

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/middleware"
)

// Activity is a process-global counter of in-flight AWS API calls. The TUI
// reads Snapshot() on every spinner tick to decide whether to animate and
// which op name to show. Callers attach it to their aws.Config via Attach().
//
// A single instance is sufficient because the binary runs one bubbletea
// program at a time. The profile switcher rebuilds the Context but shares
// the same Activity.
type Activity struct {
	inflight int64 // atomic

	mu     sync.Mutex
	lastOp string
}

// ActivitySnapshot captures the counter state at a single instant.
type ActivitySnapshot struct {
	InFlight int64
	LastOp   string
}

// NewActivity returns a fresh counter.
func NewActivity() *Activity { return &Activity{} }

// Snapshot returns a consistent view of inflight + last op.
func (a *Activity) Snapshot() ActivitySnapshot {
	a.mu.Lock()
	op := a.lastOp
	a.mu.Unlock()
	return ActivitySnapshot{
		InFlight: atomic.LoadInt64(&a.inflight),
		LastOp:   op,
	}
}

func (a *Activity) start(op string) {
	atomic.AddInt64(&a.inflight, 1)
	a.mu.Lock()
	a.lastOp = op
	a.mu.Unlock()
}

func (a *Activity) finish() {
	atomic.AddInt64(&a.inflight, -1)
}

// Attach installs a Smithy middleware on cfg so every AWS API call
// increments Activity.inflight on request-start and decrements it on
// response (success or failure). The operation name shown in the TUI is the
// most recent one to start.
func (a *Activity) Attach(cfg *aws.Config) {
	cfg.APIOptions = append(cfg.APIOptions, func(stack *middleware.Stack) error {
		return stack.Initialize.Add(
			middleware.InitializeMiddlewareFunc("scout/activity",
				func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
					op := middleware.GetOperationName(ctx)
					a.start(op)
					defer a.finish()
					return next.HandleInitialize(ctx, in)
				},
			),
			middleware.Before,
		)
	})
}
