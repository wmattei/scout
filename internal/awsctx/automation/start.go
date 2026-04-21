package automation

import (
	"context"
	"errors"
	"fmt"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/wmattei/scout/internal/awsctx"
)

// StartExecution invokes StartAutomationExecution with the given
// document and user-supplied parameters. Returns the new execution
// ID on success.
func StartExecution(ctx context.Context, ac *awsctx.Context, docName string, params map[string][]string) (string, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

	out, err := client.StartAutomationExecution(ctx, &awsssm.StartAutomationExecutionInput{
		DocumentName: &docName,
		Parameters:   params,
	})
	if err != nil {
		return "", fmt.Errorf("ssm:StartAutomationExecution (%s): %w", docName, err)
	}
	if out.AutomationExecutionId == nil {
		return "", errors.New("ssm:StartAutomationExecution: empty AutomationExecutionId")
	}
	return *out.AutomationExecutionId, nil
}
