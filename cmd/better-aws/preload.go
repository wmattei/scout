package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
)

// runPreload implements `better-aws preload <service|all>`. It resolves
// the AWS context, opens the SQLite cache for the current
// (profile, region) pair, runs the live fetch for the requested
// service(s), persists the results, prints a summary, and exits. The
// TUI never has to do a top-level refresh on launch — users either
// preload ahead of time or rely on the lazy service-scope path inside
// the TUI itself.
//
// Args are expected as the slice AFTER `better-aws preload` has already
// been stripped (so a one-element slice in the typical case).
func runPreload(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: better-aws preload <s3|ecs|td|all>")
	}
	target := strings.ToLower(args[0])

	var types []core.ResourceType
	if target == "all" {
		types = []core.ResourceType{
			core.RTypeBucket,
			core.RTypeEcsService,
			core.RTypeEcsTaskDefFamily,
		}
	} else {
		t, ok := core.ResourceTypeForAlias(target)
		if !ok {
			return fmt.Errorf("unknown service %q (try one of: s3, buckets, ecs, svc, services, td, task, taskdef, all)", target)
		}
		types = []core.ResourceType{t}
	}

	ctx := context.Background()

	awsCtx, err := awsctx.Resolve(ctx)
	if err != nil {
		return err
	}

	db, err := index.Open(awsCtx.Profile, awsCtx.Region)
	if err != nil {
		return err
	}
	defer db.Close()

	mem := index.NewMemory()
	cached, err := db.LoadAll(ctx)
	if err != nil {
		return err
	}
	mem.Load(cached)

	fmt.Printf("better-aws: preloading into %s/%s\n", awsCtx.Profile, awsCtx.Region)
	for _, t := range types {
		fmt.Printf("  %s … ", t)
		n, err := preloadOne(ctx, awsCtx, db, mem, t)
		if err != nil {
			fmt.Println("failed")
			return fmt.Errorf("%s: %w", t, err)
		}
		fmt.Printf("%d items\n", n)
	}
	return nil
}

// preloadOne runs the live fetch for a single resource type, persists
// the result, and returns the row count. Wraps the AWS list call with
// a generous timeout so a stalled ECS walk doesn't hang the
// subcommand forever.
func preloadOne(ctx context.Context, ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var (
		rs  []core.Resource
		err error
	)
	switch t {
	case core.RTypeBucket:
		rs, err = awss3.ListBuckets(ctx, ac)
	case core.RTypeEcsService:
		rs, err = awsecs.ListServices(ctx, ac)
	case core.RTypeEcsTaskDefFamily:
		rs, err = awsecs.ListTaskDefFamilies(ctx, ac)
	default:
		return 0, fmt.Errorf("unsupported resource type %v", t)
	}
	if err != nil {
		return 0, err
	}
	if err := index.Persist(ctx, db, mem, t, rs); err != nil {
		return 0, err
	}
	return len(rs), nil
}
