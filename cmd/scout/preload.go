package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/debuglog"
	"github.com/wmattei/scout/internal/index"
	"github.com/wmattei/scout/internal/services"
)

// preloadCmd builds the `preload` subcommand. `--prefix` is honoured
// server-side for S3 buckets (Prefix on ListBuckets) and ECS task-def
// families (FamilyPrefix). For ECS services the filter is applied
// client-side because the ECS API has no service-name prefix.
func preloadCmd() *cobra.Command {
	var limit int
	var prefix string

	c := &cobra.Command{
		Use:   "preload [flags] <service|all>",
		Short: "Populate the local cache from AWS",
		Long: `Fetch resources from AWS and persist them into the local SQLite cache.

Service values:
  s3 | buckets              S3 buckets
  ecs | svc | services      ECS services
  td | task | taskdef       ECS task definitions
  lambda | fn | functions   Lambda functions
  ssm | param | params      SSM parameters
  all                       Every top-level type`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			closeLog := debuglog.Init()
			defer closeLog()
			registerAWSProviders()
			return runPreload(args[0], awsctx.ListOptions{Limit: limit, Prefix: prefix})
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "max items to fetch per service (0 = unlimited)")
	c.Flags().StringVar(&prefix, "prefix", "", "name prefix filter (server-side for S3 + task defs, client-side for ECS services)")
	return c
}

func runPreload(target string, opts awsctx.ListOptions) error {
	target = strings.ToLower(target)

	var types []core.ResourceType
	if target == "all" {
		types = []core.ResourceType{
			core.RTypeBucket,
			core.RTypeEcsService,
			core.RTypeEcsTaskDefFamily,
		}
	} else {
		p, ok := services.Lookup(target)
		if !ok {
			return fmt.Errorf("unknown service %q", target)
		}
		types = []core.ResourceType{p.Type()}
	}

	ctx := context.Background()

	awsCtx, err := awsctx.Resolve(ctx)
	if err != nil {
		return err
	}

	seedTopLevelTypes()

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

	fmt.Printf("scout: preloading into %s/%s", awsCtx.Profile, awsCtx.Region)
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
// Wraps the AWS list call with a generous timeout so a stalled walk
// doesn't hang the subcommand forever.
func preloadOne(ctx context.Context, ac *awsctx.Context, db *index.DB, mem *index.Memory, t core.ResourceType, opts awsctx.ListOptions) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	p, ok := services.Get(t)
	if !ok {
		return 0, fmt.Errorf("no provider registered for resource type %v", t)
	}
	rs, err := p.ListAll(ctx, ac, opts)
	if err != nil {
		return 0, err
	}
	if err := index.Persist(ctx, db, mem, t, rs); err != nil {
		return 0, err
	}
	return len(rs), nil
}
