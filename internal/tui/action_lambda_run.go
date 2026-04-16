package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awslambda "github.com/wagnermattei/better-aws-cli/internal/awsctx/lambda"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execLambdaRun opens $EDITOR with a default "{}" payload, then invokes
// the selected Lambda function with whatever the user wrote.
func execLambdaRun(m Model) (Model, tea.Cmd) {
	if m.detailsResource.Type != core.RTypeLambdaFunction {
		m.toast = newToast("Run is only available for Lambda functions", 3*time.Second)
		return m, nil
	}

	// Create a temp file pre-filled with a default JSON payload.
	f, err := os.CreateTemp("", "better-aws-lambda-*.json")
	if err != nil {
		m.toast = newErrorToast(fmt.Sprintf("create temp file: %v", err))
		return m, nil
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		m.toast = newErrorToast(fmt.Sprintf("write temp file: %v", err))
		return m, nil
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		m.toast = newErrorToast(fmt.Sprintf("close temp file: %v", err))
		return m, nil
	}

	m.pendingEditorAction = editorActionLambdaInvoke
	m.pendingEditorPath = f.Name()
	m.pendingEditorResource = m.detailsResource
	return m, openEditorCmd(f.Name())
}

// lambdaInvokeCmd calls lambda:Invoke with the given payload and returns
// msgActionDone. If the response payload is non-empty it is written to a
// temp file and opened with the OS viewer so the user can inspect the result.
func lambdaInvokeCmd(ac *awsctx.Context, r core.Resource, payload []byte) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		result, err := awslambda.InvokeFunction(ctx, ac, r.DisplayName, payload)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("invoke failed: %v", err),
				err:   err,
			}
		}

		toast := fmt.Sprintf("invoked: status=%d", result.StatusCode)
		if result.Error != "" {
			toast += fmt.Sprintf("  error=%s", result.Error)
		}

		// If there is a meaningful response payload, write it to a temp
		// file and open it with the OS viewer so the user can inspect it.
		if len(result.Payload) > 0 && string(result.Payload) != "null" {
			if path, err := writeTempJSON(result.Payload); err == nil {
				_ = openPreview(path) // best-effort; ignore error
			}
		}

		return msgActionDone{toast: toast, err: nil}
	}
}

// writeTempJSON writes a JSON payload to a temp file with a .json extension
// and returns its path. The file is not cleaned up — it lives in the OS temp
// dir and is removed on reboot.
func writeTempJSON(data []byte) (string, error) {
	f, err := os.CreateTemp("", "better-aws-invoke-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}
