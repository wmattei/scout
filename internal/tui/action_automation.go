package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/core"
)

// execAutomationRun opens $EDITOR pre-filled with a JSON template
// derived from the document's declared parameters, then submits
// StartAutomationExecution when the user saves.
func execAutomationRun(m Model) (Model, tea.Cmd) {
	r := m.detailsResource
	if r.Type != core.RTypeSSMAutomationDocument {
		m.toast = newToast("Run is only available for Automation documents", 3*time.Second)
		return m, nil
	}

	lazy := m.lazyDetailsFor(r)
	if lazy == nil {
		m.toast = newToast("document still resolving — try again in a moment", 2*time.Second)
		return m, nil
	}

	var params []automation.ParameterInfo
	if s := lazy[automation.MetaParameters]; s != "" {
		_ = json.Unmarshal([]byte(s), &params)
	}

	templ := buildAutomationParamTemplate(params)
	body, err := json.MarshalIndent(templ, "", "  ")
	if err != nil {
		m.toast = newErrorToast("build payload template: " + err.Error())
		return m, nil
	}
	if len(params) == 0 {
		body = []byte("{}")
	}

	f, err := os.CreateTemp("", "scout-automation-*.json")
	if err != nil {
		m.toast = newErrorToast(fmt.Sprintf("create temp file: %v", err))
		return m, nil
	}
	if _, err := f.Write(body); err != nil {
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

	m.pendingEditorAction = editorActionAutomationRun
	m.pendingEditorPath = f.Name()
	m.pendingEditorResource = r
	return m, openEditorCmd(f.Name())
}

// buildAutomationParamTemplate produces a JSON-friendly skeleton from
// the document's declared parameters. StringList params render as a
// split array (comma-separated defaults) or []string{}; scalar params
// render as a plain string.
func buildAutomationParamTemplate(params []automation.ParameterInfo) map[string]interface{} {
	out := map[string]interface{}{}
	for _, p := range params {
		switch p.Type {
		case "StringList":
			if p.DefaultValue != "" {
				parts := strings.Split(p.DefaultValue, ",")
				for i := range parts {
					parts[i] = strings.TrimSpace(parts[i])
				}
				out[p.Name] = parts
			} else {
				out[p.Name] = []string{}
			}
		default:
			out[p.Name] = p.DefaultValue
		}
	}
	return out
}

// msgAutomationStarted is emitted by automationRunCmd when
// StartAutomationExecution succeeds. The handler clears inFlight,
// shows a success toast, and transitions the TUI directly into
// modeExecutionDetails so the user sees polling status updates as
// the run progresses.
type msgAutomationStarted struct {
	execID      string
	docResource core.Resource
}

// automationRunCmd decodes the edited JSON, normalises every value
// into the []string shape StartAutomationExecution requires, and fires
// the API. On success it returns msgAutomationStarted so the handler
// can jump straight into the execution details page; on failure it
// returns msgActionDone so the generic error-toast + lazy-error
// persistence flow fires.
func automationRunCmd(ac *awsctx.Context, r core.Resource, content []byte) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var raw map[string]interface{}
		if err := json.Unmarshal(content, &raw); err != nil {
			return msgActionDone{toast: "invalid JSON: " + err.Error(), err: err}
		}

		params := map[string][]string{}
		for k, v := range raw {
			switch vv := v.(type) {
			case string:
				if vv != "" {
					params[k] = []string{vv}
				}
			case []interface{}:
				arr := make([]string, 0, len(vv))
				for _, item := range vv {
					switch it := item.(type) {
					case string:
						arr = append(arr, it)
					case float64:
						arr = append(arr, strconv.FormatFloat(it, 'f', -1, 64))
					case bool:
						arr = append(arr, strconv.FormatBool(it))
					}
				}
				if len(arr) > 0 {
					params[k] = arr
				}
			case float64:
				params[k] = []string{strconv.FormatFloat(vv, 'f', -1, 64)}
			case bool:
				params[k] = []string{strconv.FormatBool(vv)}
			}
		}

		execID, err := automation.StartExecution(ctx, ac, r.Key, params)
		if err != nil {
			return msgActionDone{
				toast: fmt.Sprintf("run failed: %v", err),
				err:   err,
			}
		}
		return msgAutomationStarted{execID: execID, docResource: r}
	}
}

// shortExecID trims the verbose AWS execution UUID so it fits in the
// toast cleanly. The full ID stays in the Events row.
func shortExecID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}
