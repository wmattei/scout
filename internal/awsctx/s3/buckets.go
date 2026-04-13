// Package s3 contains better-aws's thin wrappers around the AWS S3 SDK.
// Each function returns []core.Resource ready to hand to the index layer.
package s3

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListBuckets returns every bucket visible to the current caller. One call,
// no pagination. The Region meta field is populated from the session's
// region rather than from GetBucketLocation — GetBucketLocation costs one
// extra call per bucket and Phase 1 doesn't render per-bucket region yet.
func ListBuckets(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
	client := awss3.NewFromConfig(ac.Cfg)
	out, err := client.ListBuckets(ctx, &awss3.ListBucketsInput{})
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
