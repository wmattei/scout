// Package s3 contains better-aws's thin wrappers around the AWS S3 SDK.
// Each function returns []core.Resource ready to hand to the index layer.
package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListBuckets returns buckets visible to the current caller, optionally
// capped via opts.Limit and filtered server-side via opts.Prefix. The
// Region meta field is populated from the session's region rather than
// from GetBucketLocation — GetBucketLocation costs one extra call per
// bucket and is not currently rendered.
//
// Pass `awsctx.ListOptions{}` to keep the historical behaviour
// (every bucket, no filter).
func ListBuckets(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	input := &awss3.ListBucketsInput{}
	if opts.Limit > 0 {
		input.MaxBuckets = aws.Int32(int32(opts.Limit))
	}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	out, err := client.ListBuckets(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3:ListBuckets: %w", err)
	}
	resources := make([]core.Resource, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		if b.Name == nil {
			continue
		}
		resources = append(resources, core.Resource{
			Type:        core.RTypeBucket,
			Key:         *b.Name,
			DisplayName: *b.Name,
			Meta: map[string]string{
				"region": ac.Region,
			},
		})
	}
	return resources, nil
}
