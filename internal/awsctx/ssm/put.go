package ssm

import (
	"context"
	"fmt"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/wmattei/scout/internal/awsctx"
)

// PutParameter writes a new or updated value to an SSM parameter. The
// parameter is always created/updated with Overwrite=true so re-running
// the action on an existing parameter updates its value in place rather
// than erroring on the duplicate-name check.
func PutParameter(ctx context.Context, ac *awsctx.Context, name, value, paramType string) error {
	client := awsssm.NewFromConfig(ac.Cfg)

	overwrite := true
	_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
		Name:      &name,
		Value:     &value,
		Type:      types.ParameterType(paramType),
		Overwrite: &overwrite,
	})
	if err != nil {
		return fmt.Errorf("ssm:PutParameter (%s): %w", name, err)
	}
	return nil
}
