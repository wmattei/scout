package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	awss3 "github.com/wagnermattei/better-aws-cli/internal/awsctx/s3"
	"github.com/wagnermattei/better-aws-cli/internal/core"
	"github.com/wagnermattei/better-aws-cli/internal/index"
)

// runPreload implements `better-aws preload [--limit N] [--prefix S] <service|all>`.
// It resolves the AWS context, opens the SQLite cache for the current
// (profile, region) pair, runs the live fetch for the requested
// service(s), persists the results, prints a per-type item count, and
// exits. The TUI never has to do a top-level refresh on launch.
//
// Flags are parsed via a flag.FlagSet so they can appear before or
// after the positional service argument.
//
//	better-aws preload s3
//	better-aws preload --limit 50 s3
//	better-aws preload s3 --prefix prod-
//	better-aws preload --limit 100 --prefix worker- td
//	better-aws preload all                 # every type, no filter
//	better-aws preload --limit 20 all      # cap each type at 20
//
// `--prefix` is honoured server-side for S3 buckets (Prefix on
// ListBuckets) and ECS task-def families (FamilyPrefix on
// ListTaskDefinitionFamilies). For ECS services the filter is applied
// client-side to ServiceName because ECS has no native service-name
// prefix on the API.
func runPreload(args []string) error {
	fs := flag.NewFlagSet("preload", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{}) // suppress flag's own usage spam; we print our own
	limit := fs.Int("limit", 0, "max items to fetch per service (0 = unlimited)")
	prefix := fs.String("prefix", "", "name prefix filter (server-side for S3 + task defs, client-side for ECS services)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: better-aws preload [--limit N] [--prefix S] <s3|ecs|td|all>")
	}

	positional := fs.Args()
	if len(positional) == 0 {
		return fmt.Errorf("usage: better-aws preload [--limit N] [--prefix S] <s3|ecs|td|all>")
	}
	target := strings.ToLower(positional[0])

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

	opts := awsctx.ListOptions{
		Limit:  *limit,
		Prefix: *prefix,
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

	fmt.Printf("better-aws: preloading into %s/%s", awsCtx.Profile, awsCtx.Region)
	if opts.Limit > 0 {
		fmt.Printf(" (limit=%d)", opts.Limit)
	}
	if opts.Prefix != "" {
		fmt.Printf(" (prefix=%q)", opts.Prefix)
	}
	fmt.Println()

	for _, t := range types {
		fmt.Printf("  %s … ", t)
		n, err := preloadOne(ctx, awsCtx, db, mem, t, opts)
		if err != nil {
			fmt.Println("failed")
			return fmt.Errorf("%s: %w", t, err)
		}
		fmt.Printf("%d items\n", n)
	}
	return nil
}

// preloadOne runs the live fetch for a single resource type with the
// given list options, persists the result, and returns the row count.
// Wraps the AWS list call with a generous timeout so a stalled ECS
// walk doesn't hang the subcommand forever.
func preloadOne(ctx context.Context, ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType, opts awsctx.ListOptions) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var (
		rs  []core.Resource
		err error
	)
	switch t {
	case core.RTypeBucket:
		rs, err = awss3.ListBuckets(ctx, ac, opts)
	case core.RTypeEcsService:
		rs, err = awsecs.ListServices(ctx, ac, opts)
	case core.RTypeEcsTaskDefFamily:
		rs, err = awsecs.ListTaskDefFamilies(ctx, ac, opts)
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
