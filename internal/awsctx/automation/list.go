// Package automation wraps the SSM Automation document APIs. Documents
// are the runbook definitions; executions are created by calling
// StartAutomationExecution against a document.
package automation

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListDocuments returns every SSM document whose DocumentType is
// Automation. The filter on DocumentType is server-side; an optional
// name prefix narrows further via the "Name" filter (case-sensitive
// starts-with match per SSM's filter semantics). Results are capped
// at opts.Limit (or 200 for interactive use).
func ListDocuments(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

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
		if maxResults > 50 {
			maxResults = 50
		}

		filters := []types.DocumentKeyValuesFilter{
			{Key: aws.String("DocumentType"), Values: []string{"Automation"}},
			{Key: aws.String("Owner"), Values: []string{"Self"}},
		}
		if opts.Prefix != "" {
			filters = append(filters, types.DocumentKeyValuesFilter{
				Key:    aws.String("Name"),
				Values: []string{opts.Prefix},
			})
		}

		out, err := client.ListDocuments(ctx, &awsssm.ListDocumentsInput{
			Filters:    filters,
			MaxResults: &maxResults,
			NextToken:  nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("ssm:ListDocuments: %w", err)
		}

		for _, d := range out.DocumentIdentifiers {
			if d.Name == nil {
				continue
			}
			meta := map[string]string{}
			if d.Owner != nil {
				meta[MetaOwner] = *d.Owner
			}
			if d.VersionName != nil {
				meta[MetaVersionName] = *d.VersionName
			}
			if d.DocumentVersion != nil {
				meta[MetaLatestVersion] = *d.DocumentVersion
			}
			if d.TargetType != nil {
				meta[MetaTargetType] = *d.TargetType
			}
			meta[MetaDocumentType] = string(d.DocumentType)
			if len(d.PlatformTypes) > 0 {
				pts := make([]string, 0, len(d.PlatformTypes))
				for _, p := range d.PlatformTypes {
					pts = append(pts, string(p))
				}
				meta[MetaPlatformTypes] = strings.Join(pts, ",")
			}

			resources = append(resources, core.Resource{
				Type:        core.RTypeSSMAutomationDocument,
				Key:         *d.Name,
				DisplayName: *d.Name,
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
