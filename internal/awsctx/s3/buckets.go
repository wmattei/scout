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
// Pagination: when S3 returns a ContinuationToken (which it will do
// under any non-default page size or when a prefix filter matches more
// than 10,000 buckets), this function follows it until the list is
// exhausted or the requested Limit is reached. Pass
// `awsctx.ListOptions{}` to keep the historical "every bucket, no
// filter" behaviour.
func ListBuckets(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awss3.NewFromConfig(ac.Cfg)

	var (
		resources []core.Resource
		token     *string
	)
	for {
		input := &awss3.ListBucketsInput{
			ContinuationToken: token,
		}
		if opts.Limit > 0 {
			remaining := opts.Limit - len(resources)
			if remaining <= 0 {
				break
			}
			// MaxBuckets caps at 10,000 per page.
			pageSize := int32(remaining)
			if pageSize > 10000 {
				pageSize = 10000
			}
			input.MaxBuckets = aws.Int32(pageSize)
		}
		if opts.Prefix != "" {
			input.Prefix = aws.String(opts.Prefix)
		}
		out, err := client.ListBuckets(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("s3:ListBuckets: %w", err)
		}
		for _, b := range out.Buckets {
			if b.Name == nil {
				continue
			}
			meta := map[string]string{
				"region": ac.Region,
			}
			if b.CreationDate != nil {
				meta["createdAt"] = fmt.Sprintf("%d", b.CreationDate.Unix())
			}
			resources = append(resources, core.Resource{
				Type:        core.RTypeBucket,
				Key:         *b.Name,
				DisplayName: *b.Name,
				Meta:        meta,
			})
			if opts.Limit > 0 && len(resources) >= opts.Limit {
				return resources, nil
			}
		}
		if out.ContinuationToken == nil {
			break
		}
		token = out.ContinuationToken
	}
	return resources, nil
}
