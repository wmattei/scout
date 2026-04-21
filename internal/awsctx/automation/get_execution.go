package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/wmattei/scout/internal/awsctx"
)

// ExecutionDetails is the resolved view of a single Automation
// execution used by the modeExecutionDetails screen. Separate from the
// SDK response so the TUI never touches raw SDK structs.
type ExecutionDetails struct {
	ExecutionID     string
	DocumentName    string
	DocumentVersion string
	Status          string
	Mode            string
	StartTime       time.Time
	EndTime         time.Time
	ExecutedBy      string
	FailureMessage  string
	Parameters      map[string][]string
	Outputs         map[string][]string
	Steps           []StepDetails
}

// StepDetails is one entry in an execution's StepExecutions list.
// Inputs map values are JSON-encoded strings per SDK convention;
// LambdaFunctionName/parseInput etc. unwrap them when needed.
type StepDetails struct {
	StepName       string
	StepID         string
	Action         string
	Status         string
	StartTime      time.Time
	EndTime        time.Time
	Inputs         map[string]string
	Outputs        map[string][]string
	Response       string
	ResponseCode   string
	FailureMessage string
}

// Duration returns the step's runtime. Zero EndTime with a terminal
// Status returns 0 (AWS omits the field on some failure paths); with
// a non-terminal Status it returns elapsed since StartTime.
func (s StepDetails) Duration() time.Duration {
	if s.StartTime.IsZero() {
		return 0
	}
	if !s.EndTime.IsZero() {
		return s.EndTime.Sub(s.StartTime)
	}
	if !IsTerminalStatus(s.Status) {
		return time.Since(s.StartTime)
	}
	return 0
}

// IsTerminalStatus reports whether an Automation status string
// represents a final state — no more polling needed.
func IsTerminalStatus(s string) bool {
	switch s {
	case "Success", "CompletedWithSuccess",
		"Failed", "TimedOut", "CompletedWithFailure",
		"Cancelled", "Rejected":
		return true
	}
	return false
}

// LambdaFunctionName returns the function invoked by an
// aws:invokeLambdaFunction step. Lambda steps encode FunctionName in
// the Inputs map as a JSON-quoted string (e.g. `"my-fn"`), so the
// caller has to json.Unmarshal to recover the raw name.
func (s StepDetails) LambdaFunctionName() (string, bool) {
	if s.Action != "aws:invokeLambdaFunction" {
		return "", false
	}
	raw, ok := s.Inputs["FunctionName"]
	if !ok || raw == "" {
		return "", false
	}
	var name string
	if err := json.Unmarshal([]byte(raw), &name); err != nil {
		// Some Automation templates pass FunctionName unquoted.
		return raw, true
	}
	return name, true
}

// GetExecution fetches the full execution record including every
// step's current state and I/O. Called on the initial entry to
// modeExecutionDetails and on every poll tick while the execution is
// non-terminal.
func GetExecution(ctx context.Context, ac *awsctx.Context, execID string) (*ExecutionDetails, error) {
	client := awsssm.NewFromConfig(ac.Cfg)

	out, err := client.GetAutomationExecution(ctx, &awsssm.GetAutomationExecutionInput{
		AutomationExecutionId: &execID,
	})
	if err != nil {
		return nil, fmt.Errorf("ssm:GetAutomationExecution (%s): %w", execID, err)
	}
	ae := out.AutomationExecution
	if ae == nil {
		return nil, fmt.Errorf("ssm:GetAutomationExecution (%s): nil AutomationExecution in response", execID)
	}

	d := &ExecutionDetails{
		Status:     string(ae.AutomationExecutionStatus),
		Mode:       string(ae.Mode),
		Parameters: ae.Parameters,
		Outputs:    ae.Outputs,
	}
	if ae.AutomationExecutionId != nil {
		d.ExecutionID = *ae.AutomationExecutionId
	}
	if ae.DocumentName != nil {
		d.DocumentName = *ae.DocumentName
	}
	if ae.DocumentVersion != nil {
		d.DocumentVersion = *ae.DocumentVersion
	}
	if ae.ExecutionStartTime != nil {
		d.StartTime = *ae.ExecutionStartTime
	}
	if ae.ExecutionEndTime != nil {
		d.EndTime = *ae.ExecutionEndTime
	}
	if ae.ExecutedBy != nil {
		d.ExecutedBy = *ae.ExecutedBy
	}
	if ae.FailureMessage != nil {
		d.FailureMessage = *ae.FailureMessage
	}

	for _, se := range ae.StepExecutions {
		step := StepDetails{
			Status:  string(se.StepStatus),
			Inputs:  se.Inputs,
			Outputs: se.Outputs,
		}
		if se.StepName != nil {
			step.StepName = *se.StepName
		}
		if se.StepExecutionId != nil {
			step.StepID = *se.StepExecutionId
		}
		if se.Action != nil {
			step.Action = *se.Action
		}
		if se.ExecutionStartTime != nil {
			step.StartTime = *se.ExecutionStartTime
		}
		if se.ExecutionEndTime != nil {
			step.EndTime = *se.ExecutionEndTime
		}
		if se.Response != nil {
			step.Response = *se.Response
		}
		if se.ResponseCode != nil {
			step.ResponseCode = *se.ResponseCode
		}
		if se.FailureMessage != nil {
			step.FailureMessage = *se.FailureMessage
		}
		d.Steps = append(d.Steps, step)
	}

	return d, nil
}
