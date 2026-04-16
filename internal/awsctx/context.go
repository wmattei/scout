package awsctx

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/wmattei/scout/internal/debuglog"
)

// ResolveForProfile is the same as Resolve but takes explicit profile
// and region parameters instead of reading the environment. Used by
// the TUI's profile/region switcher to hot-swap the AWS context
// without re-exec'ing the program.
//
// Both arguments MUST be non-empty; the switcher is responsible for
// picking from ListProfiles + CommonRegions (plus any pre-selected
// current region) before committing.
func ResolveForProfile(ctx context.Context, profile, region string) (*Context, error) {
	if profile == "" {
		return nil, fmt.Errorf("ResolveForProfile: profile is empty")
	}
	if region == "" {
		return nil, fmt.Errorf("ResolveForProfile: region is empty")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithSharedConfigProfile(profile),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config (profile=%s region=%s): %w", profile, region, err)
	}

	cfg.Logger = debuglog.SDKLogger()

	return &Context{
		Profile: profile,
		Region:  region,
		Cfg:     cfg,
	}, nil
}
