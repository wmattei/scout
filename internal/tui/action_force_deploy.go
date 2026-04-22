package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	awsecs "github.com/wmattei/scout/internal/awsctx/ecs"
	"github.com/wmattei/scout/internal/core"
)

// execForceDeploy enters a confirmation state instead of firing
// immediately. The user must press 'y' to proceed; any other key
// cancels. The confirmation state is tracked on the model via
// confirmingForceDeploy — the updateDetails key handler checks it
// before dispatching normal key events.
func execForceDeploy(m Model) (Model, tea.Cmd) {
	if m.detailsResource.Type != core.RTypeEcsService {
		m.toast = newToast("force deploy is only available for ECS services", 3*time.Second)
		return m, nil
	}
	cluster := m.detailsResource.Meta[awsecs.MetaClusterArn]
	service := m.detailsResource.Key
	if cluster == "" || service == "" {
		m.toast = newToast("missing cluster or service ARN", 3*time.Second)
		return m, nil
	}

	m.pendingConfirmFn = func(m Model) (Model, tea.Cmd) {
		return doForceDeploy(m)
	}
	m.toast = newToast("force new deployment on "+m.detailsResource.DisplayName+"? press y to confirm, any other key to cancel", 30*time.Second)
	return m, nil
}

// doForceDeploy is called after the user confirms with 'y'. It fires
// the actual UpdateService call.
func doForceDeploy(m Model) (Model, tea.Cmd) {
	cluster := m.detailsResource.Meta[awsecs.MetaClusterArn]
	service := m.detailsResource.Key
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
		return msgActionDone{toast: "deployment triggered", err: nil, success: true}
	}
}
