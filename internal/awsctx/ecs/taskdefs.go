package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/core"
)

// ListTaskDefFamilies returns one Resource per active task-definition family.
// The family name is the Key and DisplayName; revision is resolved lazily
// via DescribeTaskDefinition when actions need it (Phase 3).
//
// Families are listed with status=ACTIVE so retired families don't clutter
// the results. opts.Limit caps the total returned (the per-page MaxResults
// is computed against it). opts.Prefix maps directly to the API's
// FamilyPrefix field, so filtering happens server-side.
//
// Pass `awsctx.ListOptions{}` for the historical behaviour.
func ListTaskDefFamilies(ctx context.Context, ac *awsctx.Context, opts awsctx.ListOptions) ([]core.Resource, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	// Default per-page MaxResults is 100; if the caller capped lower we
	// fetch in smaller pages so an early break doesn't over-fetch.
	pageSize := int32(100)
	if opts.Limit > 0 && int32(opts.Limit) < pageSize {
		pageSize = int32(opts.Limit)
	}

	var families []string
	var next *string
	for {
		input := &awsecs.ListTaskDefinitionFamiliesInput{
			Status:     ecstypes.TaskDefinitionFamilyStatusActive,
			NextToken:  next,
			MaxResults: aws.Int32(pageSize),
		}
		if opts.Prefix != "" {
			input.FamilyPrefix = aws.String(opts.Prefix)
		}
		out, err := client.ListTaskDefinitionFamilies(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("ecs:ListTaskDefinitionFamilies: %w", err)
		}
		families = append(families, out.Families...)
		if opts.Limit > 0 && len(families) >= opts.Limit {
			families = families[:opts.Limit]
			break
		}
		if out.NextToken == nil {
			break
		}
		next = out.NextToken
	}

	resources := make([]core.Resource, 0, len(families))
	for _, fam := range families {
		resources = append(resources, core.Resource{
			Type:        core.RTypeEcsTaskDefFamily,
			Key:         fam,
			DisplayName: fam,
			Meta:        map[string]string{}, // revision + containers resolved lazily
		})
	}
	return resources, nil
}
