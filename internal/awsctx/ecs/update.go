package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// ForceDeployment triggers a rolling deployment on an ECS service by
// calling UpdateService with ForceNewDeployment=true. Neither the task
// definition nor the desired count is changed.
func ForceDeployment(ctx context.Context, ac *awsctx.Context, clusterArn, serviceArn string) error {
	client := awsecs.NewFromConfig(ac.Cfg)
	_, err := client.UpdateService(ctx, &awsecs.UpdateServiceInput{
		Cluster:            aws.String(clusterArn),
		Service:            aws.String(serviceArn),
		ForceNewDeployment: true,
	})
	if err != nil {
		return fmt.Errorf("ecs:UpdateService (service=%s): %w", serviceArn, err)
	}
	return nil
}
