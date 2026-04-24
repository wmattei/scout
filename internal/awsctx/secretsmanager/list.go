// Package secretsmanager contains scout's thin wrappers around the AWS
// Secrets Manager SDK.
package secretsmanager

import (
	"context"
	"fmt"
	"strconv"

	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListSecrets lists Secrets Manager secrets using ListSecrets. If
// opts.Prefix is set, a Name filter is applied server-side (Secrets
// Manager's Name filter is a case-sensitive "starts with" match). If
// opts.Limit is set, results are capped. Without a limit the adapter
// caps at 200 internally so interactive scope fetches don't paginate
// through unbounded account state.
func ListSecrets(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awssm.NewFromConfig(ac.Cfg)

	limit := opts.Limit
	if limit <= 0 {
		limit = 200
	}

	var resources []core.Resource
	var nextToken *string

	for {
		remaining := limit - len(resources)
		if remaining <= 0 {
			break
		}
		maxResults := int32(remaining)
		if maxResults > 100 { // API hard limit for ListSecrets
			maxResults = 100
		}

		input := &awssm.ListSecretsInput{
			NextToken:  nextToken,
			MaxResults: &maxResults,
		}
		if opts.Prefix != "" {
			input.Filters = []types.Filter{
				{
					Key:    types.FilterNameStringTypeName,
					Values: []string{opts.Prefix},
				},
			}
		}

		out, err := client.ListSecrets(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("secretsmanager:ListSecrets: %w", err)
		}

		for _, s := range out.SecretList {
			if s.Name == nil {
				continue
			}

			meta := map[string]string{}
			if s.ARN != nil {
				meta[MetaARN] = *s.ARN
			}
			if s.Description != nil {
				meta[MetaDescription] = *s.Description
			}
			if s.KmsKeyId != nil {
				meta[MetaKmsKeyID] = *s.KmsKeyId
			}
			if s.LastChangedDate != nil {
				meta[MetaLastChanged] = strconv.FormatInt(s.LastChangedDate.Unix(), 10)
			}
			if s.LastAccessedDate != nil {
				meta[MetaLastAccessed] = strconv.FormatInt(s.LastAccessedDate.Unix(), 10)
			}
			if s.LastRotatedDate != nil {
				meta[MetaLastRotated] = strconv.FormatInt(s.LastRotatedDate.Unix(), 10)
			}
			if s.RotationEnabled != nil && *s.RotationEnabled {
				meta[MetaRotationEnabled] = "true"
			}

			resources = append(resources, core.Resource{
				Key:         *s.Name,
				DisplayName: *s.Name,
				Meta:        meta,
			})

			if len(resources) >= limit {
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
