// Package ssm contains better-aws's thin wrappers around the AWS SSM SDK.
package ssm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListParameters lists SSM Parameter Store parameters using DescribeParameters.
// If opts.Prefix is set, a ParameterFilters Name+BeginsWith filter is applied
// server-side. If opts.Limit is set, results are capped (max 50 per page).
func ListParameters(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

	var resources []core.Resource
	var nextToken *string

	for {
		input := &awsssm.DescribeParametersInput{
			NextToken: nextToken,
		}

		// Cap page size at 50 (API hard limit).
		if opts.Limit > 0 {
			remaining := opts.Limit - len(resources)
			if remaining <= 0 {
				break
			}
			maxResults := int32(remaining)
			if maxResults > 50 {
				maxResults = 50
			}
			input.MaxResults = &maxResults
		}

		// Server-side prefix filter via ParameterFilters.
		if opts.Prefix != "" {
			input.ParameterFilters = []types.ParameterStringFilter{
				{
					Key:    aws.String("Name"),
					Option: aws.String("BeginsWith"),
					Values: []string{opts.Prefix},
				},
			}
		}

		out, err := client.DescribeParameters(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("ssm:DescribeParameters: %w", err)
		}

		for _, p := range out.Parameters {
			if p.Name == nil {
				continue
			}

			meta := map[string]string{}
			meta["type"] = string(p.Type)
			if p.LastModifiedDate != nil {
				meta["lastModified"] = fmt.Sprintf("%d", p.LastModifiedDate.Unix())
			}
			if p.Description != nil {
				meta["description"] = *p.Description
			}
			meta["tier"] = string(p.Tier)
			meta["version"] = fmt.Sprintf("%d", p.Version)

			resources = append(resources, core.Resource{
				Type:        core.RTypeSSMParameter,
				Key:         *p.Name,
				DisplayName: *p.Name,
				Meta:        meta,
			})

			if opts.Limit > 0 && len(resources) >= opts.Limit {
				break
			}
		}

		if opts.Limit > 0 && len(resources) >= opts.Limit {
			break
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return resources, nil
}
