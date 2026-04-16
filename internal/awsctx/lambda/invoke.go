package lambda

import (
	"context"
	"encoding/base64"
	"fmt"

	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
)

// InvokeResult holds the response from a Lambda function invocation.
type InvokeResult struct {
	StatusCode int32
	Payload    []byte
	Error      string // FunctionError field; "" when there is no error
	LogResult  string // base64-encoded tail of the execution log
}

// InvokeFunction invokes a Lambda function synchronously with the given
// JSON payload. LogType is set to Tail so the caller can inspect the
// last ~4 KB of the execution log via InvokeResult.LogResult.
func InvokeFunction(ctx context.Context, ac *awsctx.Context, functionName string, payload []byte) (*InvokeResult, error) {
	client := awslambda.NewFromConfig(ac.Cfg)

	out, err := client.Invoke(ctx, &awslambda.InvokeInput{
		FunctionName: &functionName,
		Payload:      payload,
		LogType:      types.LogTypeTail,
	})
	if err != nil {
		return nil, fmt.Errorf("lambda:Invoke (%s): %w", functionName, err)
	}

	result := &InvokeResult{
		StatusCode: out.StatusCode,
		Payload:    out.Payload,
	}
	if out.FunctionError != nil {
		result.Error = *out.FunctionError
	}
	if out.LogResult != nil {
		// LogResult is already base64-encoded by the API; decode it so
		// callers get the raw log text and can re-encode or display as-is.
		decoded, err := base64.StdEncoding.DecodeString(*out.LogResult)
		if err == nil {
			result.LogResult = string(decoded)
		} else {
			// If decoding fails, pass through the raw value.
			result.LogResult = *out.LogResult
		}
	}
	return result, nil
}
