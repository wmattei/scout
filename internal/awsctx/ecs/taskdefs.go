package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// ListTaskDefFamilies returns one Resource per active task-definition family.
// The family name is the Key and DisplayName; revision is resolved lazily
// via DescribeTaskDefinition when actions need it (Phase 3).
//
// Families are listed with status=ACTIVE so retired families don't clutter
// the results.
func ListTaskDefFamilies(ctx context.Context, ac *awsctx.Context) ([]core.Resource, error) {
	client := awsecs.NewFromConfig(ac.Cfg)

	var families []string
	var next *string
	for {
		out, err := client.ListTaskDefinitionFamilies(ctx, &awsecs.ListTaskDefinitionFamiliesInput{
			Status:     ecstypes.TaskDefinitionFamilyStatusActive,
			NextToken:  next,
			MaxResults: aws.Int32(100),
		})
		if err != nil {
			return nil, fmt.Errorf("ecs:ListTaskDefinitionFamilies: %w", err)
		}
		families = append(families, out.Families...)
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
