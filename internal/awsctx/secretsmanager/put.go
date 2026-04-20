package secretsmanager

import (
	"context"
	"fmt"

	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/wmattei/scout/internal/awsctx"
)

// PutSecretValue stores a new version of the secret and promotes it to
// AWSCURRENT. The SDK generates a ClientRequestToken when one isn't
// supplied, which is the behaviour we want for interactive edits.
func PutSecretValue(ctx context.Context, ac *awsctx.Context, secretID, value string) error {
	client := awssm.NewFromConfig(ac.Cfg)

	_, err := client.PutSecretValue(ctx, &awssm.PutSecretValueInput{
		SecretId:     &secretID,
		SecretString: &value,
	})
	if err != nil {
		return fmt.Errorf("secretsmanager:PutSecretValue (%s): %w", secretID, err)
	}
	return nil
}
