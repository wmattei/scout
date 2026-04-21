package automation

import (
	"context"
	"fmt"
	"time"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/wmattei/scout/internal/awsctx"
)

// ExecutionInfo is one row in the Document details Events zone — a
// historical Automation execution against the document. Times are
// stored as time.Time for JSON round-tripping; the zero value signals
// "no value" (e.g. still running → no EndTime).
type ExecutionInfo struct {
	ExecutionID    string    `json:"id"`
	Status         string    `json:"status"`
	Mode           string    `json:"mode,omitempty"`
	StartTime      time.Time `json:"start"`
	EndTime        time.Time `json:"end,omitempty"`
	ExecutedBy     string    `json:"by,omitempty"`
	FailureMessage string    `json:"failure,omitempty"`
}

// ListExecutions returns the most recent executions of the named
// document. Server-side filter is DocumentNamePrefix (matches any
// name starting with docName); we additionally filter client-side for
// exact name match so "MyDoc" results don't include "MyDoc2".
func ListExecutions(ctx context.Context, ac *awsctx.Context, docName string, limit int) ([]ExecutionInfo, error) {
	client := awsssm.NewFromConfig(ac.Cfg)
	if limit <= 0 {
		limit = 10
	}

	// MaxResults ceiling per the API is 50; request the lesser.
	maxResults := int32(limit * 2) // over-fetch to compensate for client-side exact filter
	if maxResults > 50 {
		maxResults = 50
	}
	if maxResults < 5 {
		maxResults = 5
	}

	out, err := client.DescribeAutomationExecutions(ctx, &awsssm.DescribeAutomationExecutionsInput{
		Filters: []types.AutomationExecutionFilter{
			{
				Key:    types.AutomationExecutionFilterKeyDocumentNamePrefix,
				Values: []string{docName},
			},
		},
		MaxResults: &maxResults,
	})
	if err != nil {
		return nil, fmt.Errorf("ssm:DescribeAutomationExecutions (%s): %w", docName, err)
	}

	execs := make([]ExecutionInfo, 0, len(out.AutomationExecutionMetadataList))
	for _, e := range out.AutomationExecutionMetadataList {
		if e.DocumentName == nil || *e.DocumentName != docName {
			continue
		}
		ei := ExecutionInfo{
			Status: string(e.AutomationExecutionStatus),
			Mode:   string(e.Mode),
		}
		if e.AutomationExecutionId != nil {
			ei.ExecutionID = *e.AutomationExecutionId
		}
		if e.ExecutionStartTime != nil {
			ei.StartTime = *e.ExecutionStartTime
		}
		if e.ExecutionEndTime != nil {
			ei.EndTime = *e.ExecutionEndTime
		}
		if e.ExecutedBy != nil {
			ei.ExecutedBy = *e.ExecutedBy
		}
		if e.FailureMessage != nil {
			ei.FailureMessage = *e.FailureMessage
		}
		execs = append(execs, ei)
		if len(execs) >= limit {
			break
		}
	}
	return execs, nil
}
