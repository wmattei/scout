package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnermattei/better-aws-cli/internal/awsctx"
	awsecs "github.com/wagnermattei/better-aws-cli/internal/awsctx/ecs"
	"github.com/wagnermattei/better-aws-cli/internal/core"
)

// execForceDeploy fires an ECS UpdateService(ForceNewDeployment=true) on
// the current details resource. Sets the in-flight lock so no other
// action can run concurrently; the lock is released in the msgActionDone
// handler. msgActionDone itself is declared in actions.go alongside the
// Action type so every action file can reach it without import cycles.
func execForceDeploy(m Model) (Model, tea.Cmd) {
	if m.detailsResource.Type != core.RTypeEcsService {
		m.toast = newToast("force deploy is only available for ECS services", 3*time.Second)
		return m, nil
	}
	cluster := m.detailsResource.Meta["clusterArn"]
	service := m.detailsResource.Key
	if cluster == "" || service == "" {
		m.toast = newToast("missing cluster or service ARN", 3*time.Second)
		return m, nil
	}

	m.inFlight = true
	m.inFlightLabel = "forcing new deployment…"
	m.toast = newToast("forcing new deployment…", 10*time.Second)
	ac := m.awsCtx
	return m, forceDeployCmd(ac, cluster, service)
}

// forceDeployCmd wraps the ECS UpdateService call in a tea.Cmd.
func forceDeployCmd(ac *awsctx.Context, clusterArn, serviceArn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := awsecs.ForceDeployment(ctx, ac, clusterArn, serviceArn)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("force deploy failed: %v", err),
				err:   err,
			}
		}
		return msgActionDone{toast: "deployment triggered", err: nil}
	}
}
