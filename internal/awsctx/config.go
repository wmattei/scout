// Package awsctx wraps aws-sdk-go-v2 configuration loading and exposes a
// single "Context" value that carries everything downstream AWS adapters
// need: the loaded aws.Config, the resolved profile name, the resolved
// region, and (lazily) the caller-identity account ID.
package awsctx

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/wmattei/scout/internal/debuglog"
)

// Context is the resolved AWS environment for the current session. One is
// created on startup; the profile switcher rebuilds the Context on each switch.
type Context struct {
	Profile string
	Region  string
	Cfg     aws.Config
}

// Resolve loads an aws.Config using the default SDK chain with the following
// precedence for profile and region:
//
//	profile: AWS_PROFILE > AWS_DEFAULT_PROFILE > "default"
//	region:  AWS_REGION  > AWS_DEFAULT_REGION  > profile's configured region
//
// If none of the above yield a region, Resolve returns an error and the
// caller exits. A modal fallback picker is planned for when no region resolves.
func Resolve(ctx context.Context) (*Context, error) {
	profile := firstNonEmpty(os.Getenv("AWS_PROFILE"), os.Getenv("AWS_DEFAULT_PROFILE"), "default")

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithSharedConfigProfile(profile),
	}
	if region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")); region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config (profile=%s): %w", profile, err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("no region resolved for profile %q — set AWS_REGION or configure 'region' in ~/.aws/config", profile)
	}

	// Route the SDK logger through debuglog. When SCOUT_DEBUG is
	// unset the adapter is smithy's Nop{}, so no output hits the
	// terminal and the alt-screen frame stays stable. With the env
	// var set, SDK records flow into
	// $XDG_CACHE_HOME/scout/debug.log alongside app-level
	// events.
	cfg.Logger = debuglog.SDKLogger()

	return &Context{
		Profile: profile,
		Region:  cfg.Region,
		Cfg:     cfg,
	}, nil
}

// CallerIdentity fetches the account ID via sts:GetCallerIdentity. Called at
// most once per session to render the status bar; result is cached by the
// caller.
func (c *Context) CallerIdentity(ctx context.Context) (string, error) {
	out, err := sts.NewFromConfig(c.Cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("sts:GetCallerIdentity: %w", err)
	}
	if out.Account == nil {
		return "", fmt.Errorf("sts:GetCallerIdentity returned no account")
	}
	return *out.Account, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
