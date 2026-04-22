package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wmattei/scout/internal/awsctx"
	"github.com/wmattei/scout/internal/awsctx/automation"
	"github.com/wmattei/scout/internal/core"
	"github.com/wmattei/scout/internal/services"
)

// ----------------------------------------------------------------------
// Messages
// ----------------------------------------------------------------------

// msgExecutionFetched carries a completed GetAutomationExecution
// response into the update loop. Epoch is compared against
// Model.exec.PollEpoch to drop stale responses after the user
// leaves the mode.
type msgExecutionFetched struct {
	execID string
	data   *automation.ExecutionDetails
	err    error
	epoch  int
}

// msgExecutionStepLogs carries a per-step log snapshot. The handler
// writes lines into Model.exec.StepLogs keyed by StepID.
type msgExecutionStepLogs struct {
	stepID string
	lines  []string
	err    error
	epoch  int
}

// msgExecutionPollTick fires on a 3-second ticker while the execution
// is non-terminal. The handler re-fires fetchExecutionCmd and re-arms
// the ticker until status becomes terminal.
type msgExecutionPollTick struct {
	epoch int
}

// ----------------------------------------------------------------------
// Commands
// ----------------------------------------------------------------------

func fetchExecutionCmd(ac *awsctx.Context, execID string, epoch int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data, err := automation.GetExecution(ctx, ac, execID)
		return msgExecutionFetched{execID: execID, data: data, err: err, epoch: epoch}
	}
}

func fetchStepLogsCmd(ac *awsctx.Context, step automation.StepDetails, epoch int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lines, err := automation.StepLogSnapshot(ctx, ac, step, 30)
		return msgExecutionStepLogs{stepID: step.StepID, lines: lines, err: err, epoch: epoch}
	}
}

func executionPollTickCmd(epoch int) tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return msgExecutionPollTick{epoch: epoch}
	})
}

// ----------------------------------------------------------------------
// Entry
// ----------------------------------------------------------------------

// executionPostTerminalGrace is the number of polling ticks (of 3s
// each — so ~30 seconds) that the execution view keeps polling and
// re-fetching Lambda step logs AFTER the overall execution reaches
// a terminal status. Lambda invocation logs can lag CloudWatch by
// several seconds, so a step that fails fast often has no log events
// visible yet when the execution itself flips to Failed. The grace
// window lets those delayed log batches catch up without the user
// having to manually refresh.
const executionPostTerminalGrace = 10

// enterExecutionDetails transitions the TUI from modeDetails into
// modeExecutionDetails for the given execution ID. Increments the
// poll epoch so any in-flight fetches from a prior entry are ignored
// when they land. Fires a fetch + pre-warmed poll tick.
func enterExecutionDetails(m Model, execID string) (Model, tea.Cmd) {
	m.exec.PollEpoch++
	m.exec.ID = execID
	m.exec.Document = m.detailsResource
	m.exec.Data = nil
	m.exec.StepSel = 0
	m.exec.StepLogs = map[string][]string{}
	m.exec.Error = ""
	m.exec.GraceRemaining = executionPostTerminalGrace
	m.mode = modeExecutionDetails
	resizeExecutionViewport(&m)
	return m, fetchExecutionCmd(m.awsCtx, execID, m.exec.PollEpoch)
}

// resizeExecutionViewport adjusts the execution viewport to match
// the current frame dimensions minus the input bar, dividers, and
// status bar. Called on entry + on tea.WindowSizeMsg.
func resizeExecutionViewport(m *Model) {
	if m.width < 40 || m.height < 10 {
		return
	}
	// Mirrors the bodyHeight computation in view.go: frame − input − 2 dividers − status.
	bodyH := m.height - 4
	if bodyH < 3 {
		bodyH = 3
	}
	m.exec.Viewport.Width = m.width
	m.exec.Viewport.Height = bodyH
}

// ----------------------------------------------------------------------
// Update handler
// ----------------------------------------------------------------------

// updateExecutionDetails handles key + message events while in
// modeExecutionDetails.
func updateExecutionDetails(m Model, msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// Invalidate any pending polls and return to the
			// document details page. The runbook's AlwaysRefresh
			// flag on re-entry re-fetches executions so the user
			// sees any state changes that happened while they
			// were inside the execution view.
			m.exec.PollEpoch++
			m.mode = modeDetails
			m.toast = newToast("", 0)
			return m, nil
		case "up", "k":
			if m.exec.StepSel > 0 {
				m.exec.StepSel--
			}
			renderExecutionContent(&m)
			return m, nil
		case "down", "j":
			if m.exec.Data != nil && m.exec.StepSel < len(m.exec.Data.Steps)-1 {
				m.exec.StepSel++
			}
			renderExecutionContent(&m)
			return m, nil
		case "pgup":
			m.exec.Viewport.HalfViewUp()
			return m, nil
		case "pgdown":
			m.exec.Viewport.HalfViewDown()
			return m, nil
		}
	}
	// Fall through: let the viewport handle mousewheel + other
	// navigation keys (home/end etc.).
	var cmd tea.Cmd
	m.exec.Viewport, cmd = m.exec.Viewport.Update(msg)
	return m, cmd
}

// ----------------------------------------------------------------------
// Message handlers — called from the top-level Update dispatcher.
// ----------------------------------------------------------------------

func handleExecutionFetched(m Model, msg msgExecutionFetched) (Model, tea.Cmd) {
	if msg.epoch != m.exec.PollEpoch {
		return m, nil // stale — user left the mode
	}
	if msg.err != nil {
		m.exec.Error = msg.err.Error()
		renderExecutionContent(&m)
		return m, nil
	}
	m.exec.Data = msg.data
	m.exec.Error = ""
	if m.exec.StepSel >= len(msg.data.Steps) {
		m.exec.StepSel = len(msg.data.Steps) - 1
		if m.exec.StepSel < 0 {
			m.exec.StepSel = 0
		}
	}

	terminal := automation.IsTerminalStatus(msg.data.Status)
	if terminal {
		if m.exec.GraceRemaining > 0 {
			m.exec.GraceRemaining--
		}
	} else {
		// Any time the execution is still in-flight the grace
		// counter resets — we only enter the post-terminal
		// catch-up window AFTER the run has actually completed.
		m.exec.GraceRemaining = executionPostTerminalGrace
	}
	inGrace := terminal && m.exec.GraceRemaining > 0

	renderExecutionContent(&m)

	var cmds []tea.Cmd
	// Fan out step log fetches. Running or pending steps fetch
	// on every tick so the log tile stays warm. Terminal steps
	// normally cache after the first fetch — but during the
	// post-terminal grace window we keep re-fetching so Lambda
	// logs that arrive after the step itself finishes still show
	// up (a common case for fail-fast invocations).
	for _, step := range msg.data.Steps {
		if _, ok := automation.StepLogGroup(step); !ok {
			continue
		}
		_, cached := m.exec.StepLogs[step.StepID]
		stepTerminal := automation.IsTerminalStatus(step.Status)
		if cached && stepTerminal && !inGrace {
			continue
		}
		cmds = append(cmds, fetchStepLogsCmd(m.awsCtx, step, m.exec.PollEpoch))
	}
	// Keep ticking while the run is live OR we're in the
	// post-terminal grace window. Once grace hits 0 for a
	// terminal run we drop the ticker and the view stops auto-
	// refreshing.
	if !terminal || m.exec.GraceRemaining > 0 {
		cmds = append(cmds, executionPollTickCmd(m.exec.PollEpoch))
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

func handleExecutionStepLogs(m Model, msg msgExecutionStepLogs) (Model, tea.Cmd) {
	if msg.epoch != m.exec.PollEpoch {
		return m, nil
	}
	if m.exec.StepLogs == nil {
		m.exec.StepLogs = map[string][]string{}
	}
	if msg.err == nil {
		m.exec.StepLogs[msg.stepID] = msg.lines
		renderExecutionContent(&m)
	}
	return m, nil
}

func handleExecutionPollTick(m Model, msg msgExecutionPollTick) (Model, tea.Cmd) {
	if msg.epoch != m.exec.PollEpoch || m.mode != modeExecutionDetails {
		return m, nil
	}
	return m, fetchExecutionCmd(m.awsCtx, m.exec.ID, m.exec.PollEpoch)
}

// ----------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------

// renderExecutionDetails is the View-time entry for modeExecutionDetails.
// It delegates to renderExecutionContent (which populates the viewport)
// and returns the viewport's visible frame.
func renderExecutionDetails(m Model, width, height int) string {
	m.exec.Viewport.Width = width
	m.exec.Viewport.Height = height
	renderExecutionContent(&m)
	return m.exec.Viewport.View()
}

// renderExecutionContent assembles the entire execution body (header,
// inputs block, per-step tiles) and writes it into the viewport's
// scrollback. Also scrolls the viewport so the currently-selected
// step stays visible.
func renderExecutionContent(m *Model) {
	if m.exec.Viewport.Width == 0 {
		resizeExecutionViewport(m)
	}
	width := m.exec.Viewport.Width
	if width < 40 {
		width = 40
	}

	var b strings.Builder
	b.WriteString(renderExecutionHeader(*m, width))
	b.WriteString("\n\n")
	if body := renderExecutionInputs(*m, width); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	stepStarts := make([]int, 0)
	currentLine := strings.Count(b.String(), "\n")
	if m.exec.Data != nil {
		for i, step := range m.exec.Data.Steps {
			tile := renderStepTile(*m, step, i, width)
			stepStarts = append(stepStarts, currentLine)
			b.WriteString(tile)
			b.WriteString("\n\n")
			currentLine += strings.Count(tile, "\n") + 2
		}
	}

	content := strings.TrimRight(b.String(), "\n")
	m.exec.Viewport.SetContent(content)

	// Auto-scroll so the selected step is visible.
	if m.exec.StepSel >= 0 && m.exec.StepSel < len(stepStarts) {
		target := stepStarts[m.exec.StepSel]
		currentYOffset := m.exec.Viewport.YOffset
		visibleEnd := currentYOffset + m.exec.Viewport.Height
		if target < currentYOffset {
			m.exec.Viewport.SetYOffset(target)
		} else if target >= visibleEnd-2 {
			m.exec.Viewport.SetYOffset(target - m.exec.Viewport.Height + 4)
		}
	}
}

// renderExecutionHeader builds the top block: execution identity +
// overall status badge + timing.
func renderExecutionHeader(m Model, width int) string {
	data := m.exec.Data
	var b strings.Builder

	if m.exec.Error != "" {
		errLine := styleExecErr.Render("fetch failed: " + m.exec.Error)
		return renderExecZone("EXECUTION "+shortExecID(m.exec.ID), errLine, width, 0, false)
	}

	if data == nil {
		return renderExecZone("EXECUTION "+shortExecID(m.exec.ID),
			styleExecDim.Render(spinnerFrame(m.spinTick)+"  loading execution…"),
			width, 0, false)
	}

	b.WriteString(styleExecLabel.Render("id   "))
	b.WriteString(data.ExecutionID)
	b.WriteString("\n")
	b.WriteString(styleExecLabel.Render("doc  "))
	b.WriteString(data.DocumentName)
	if data.DocumentVersion != "" {
		b.WriteString(styleExecDim.Render("  v" + data.DocumentVersion))
	}
	b.WriteString("\n")
	b.WriteString(styleExecLabel.Render("when "))
	if !data.StartTime.IsZero() {
		b.WriteString(data.StartTime.Format("2006-01-02 15:04:05"))
	}
	if dur := executionDuration(data); dur > 0 {
		b.WriteString(styleExecDim.Render("   " + humanExecDuration(dur)))
	}
	b.WriteString("\n")
	b.WriteString(styleExecLabel.Render("by   "))
	if data.ExecutedBy != "" {
		b.WriteString(data.ExecutedBy)
	} else {
		b.WriteString(styleExecDim.Render("—"))
	}
	b.WriteString("\n")
	b.WriteString(styleExecLabel.Render("    "))
	b.WriteString(executionStatusPill(m, data.Status))
	if data.FailureMessage != "" {
		b.WriteString("\n")
		b.WriteString(styleExecLabel.Render("err  "))
		b.WriteString(styleExecErr.Render(data.FailureMessage))
	}

	return renderExecZone("EXECUTION", b.String(), width, 0, false)
}

// renderExecutionInputs renders the execution's input parameters
// as pretty-printed colorized JSON inside its own zone. Returns ""
// when the execution has no parameters.
func renderExecutionInputs(m Model, width int) string {
	if m.exec.Data == nil || len(m.exec.Data.Parameters) == 0 {
		return ""
	}
	obj := map[string]interface{}{}
	for k, v := range m.exec.Data.Parameters {
		if len(v) == 1 {
			obj[k] = v[0]
		} else {
			obj[k] = v
		}
	}
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return ""
	}
	body := colorizeExecJSON(string(raw))
	return renderExecZone("INPUTS", body, width, 0, false)
}

// renderStepTile renders one step's bordered block. When step index
// matches exec.StepSel, the border is highlighted.
func renderStepTile(m Model, step automation.StepDetails, index, width int) string {
	selected := index == m.exec.StepSel

	title := fmt.Sprintf("%d · %s · %s", index+1, step.StepName, step.Action)
	var bodyBuf strings.Builder

	// Status + duration header line.
	statusLine := executionStatusPill(m, step.Status)
	if dur := step.Duration(); dur > 0 {
		statusLine += styleExecDim.Render("   " + humanExecDuration(dur))
	}
	bodyBuf.WriteString(statusLine)
	bodyBuf.WriteString("\n\n")

	// For Lambda steps, show the log snapshot. For non-Lambda
	// steps, show Response/Failure/Outputs as plain text.
	_, isLambda := automation.StepLogGroup(step)
	if isLambda {
		lines, cached := m.exec.StepLogs[step.StepID]
		switch {
		case !cached:
			bodyBuf.WriteString(styleExecDim.Render(spinnerFrame(m.spinTick) + "  loading logs…"))
		case len(lines) == 0:
			bodyBuf.WriteString(styleExecDim.Render("(no log events in window)"))
		default:
			for _, ln := range lines {
				bodyBuf.WriteString(ln)
				bodyBuf.WriteString("\n")
			}
		}
	} else {
		bodyBuf.WriteString(renderNonLambdaStepBody(step))
	}

	if step.FailureMessage != "" {
		bodyBuf.WriteString("\n")
		bodyBuf.WriteString(styleExecErr.Render("✗ " + step.FailureMessage))
	}

	return renderExecZone(title, strings.TrimRight(bodyBuf.String(), "\n"), width, 0, selected)
}

// renderNonLambdaStepBody shows outputs + response for steps we
// don't currently pull CloudWatch logs for.
func renderNonLambdaStepBody(step automation.StepDetails) string {
	var b strings.Builder
	if step.Response != "" {
		b.WriteString(styleExecLabel.Render("response  "))
		b.WriteString(step.Response)
		b.WriteString("\n")
	}
	if step.ResponseCode != "" && step.ResponseCode != "0" {
		b.WriteString(styleExecLabel.Render("code      "))
		b.WriteString(step.ResponseCode)
		b.WriteString("\n")
	}
	if len(step.Outputs) > 0 {
		keys := make([]string, 0, len(step.Outputs))
		for k := range step.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := strings.Join(step.Outputs[k], ", ")
			b.WriteString(styleExecLabel.Render(padRightPlain(k, 10)))
			b.WriteString(" ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return styleExecDim.Render("(no outputs)")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderExecZone wraps body in a bordered block with a dim header
// label, the same way renderZoneBlock does for the document details
// view. When selected=true the border is rendered in the accent color.
func renderExecZone(header, body string, width, height int, selected bool) string {
	styleW := width - 2
	if styleW < 1 {
		styleW = 1
	}
	style := styleZoneBorder.Width(styleW)
	if selected {
		style = style.BorderForeground(lipgloss.AdaptiveColor{Light: "#0087D7", Dark: "#5FAFFF"})
	}
	if height > 0 {
		styleH := height - 2
		if styleH < 1 {
			styleH = 1
		}
		style = style.Height(styleH)
	}
	block := style.Render(body)
	return overlayZoneHeader(block, header)
}

// executionStatusPill returns a prominent badge for the current
// status. In-progress states include the frame spinner.
func executionStatusPill(m Model, status string) string {
	spin := spinnerFrame(m.spinTick)
	switch status {
	case "Success", "CompletedWithSuccess":
		return styleExecOk.Render("✓ " + status)
	case "Failed", "TimedOut", "CompletedWithFailure", "Rejected":
		return styleExecErr.Render("✗ " + status)
	case "InProgress", "Pending", "Waiting", "RunbookInProgress", "PendingApproval", "Scheduled":
		return styleExecWarn.Render(spin + " " + status)
	case "Cancelling", "Cancelled":
		return styleExecDim.Render("⊘ " + status)
	case "":
		return styleExecDim.Render("—")
	default:
		return status
	}
}

// executionDuration returns runtime for the whole execution; for
// non-terminal runs it's elapsed since StartTime.
func executionDuration(d *automation.ExecutionDetails) time.Duration {
	if d == nil || d.StartTime.IsZero() {
		return 0
	}
	if !d.EndTime.IsZero() {
		return d.EndTime.Sub(d.StartTime)
	}
	if !automation.IsTerminalStatus(d.Status) {
		return time.Since(d.StartTime)
	}
	return 0
}

// humanExecDuration mirrors humanDuration in the automation adapter
// but lives in the TUI so execution rendering doesn't pull the
// provider-local helper back out across packages.
func humanExecDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d / time.Minute)
		s := int(d/time.Second) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d / time.Hour)
	m := int(d/time.Minute) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// ----------------------------------------------------------------------
// Styles
// ----------------------------------------------------------------------

var (
	styleExecLabel = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleExecDim   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"})
	styleExecOk    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#00875F", Dark: "#5FD7AF"})
	styleExecErr   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#FF5F5F"})
	styleExecWarn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
)

// ----------------------------------------------------------------------
// Small JSON colorizer local to execution rendering. Duplicates the
// one in automation/render.go because details.go's inputs are
// pre-rendered by the provider and this mode doesn't go through the
// provider's DetailRows. A shared package for this would be nice in
// the future — not worth the dep layering cost for Phase 2.
// ----------------------------------------------------------------------

func colorizeExecJSON(s string) string {
	var out strings.Builder
	i := 0
	key := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#005FAF", Dark: "#87D7FF"})
	str := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#008700", Dark: "#87D787"})
	num := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#875F00", Dark: "#FFD75F"})
	kw := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5F00AF", Dark: "#AF87FF"})
	punct := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#626262"})

	for i < len(s) {
		c := s[i]
		switch {
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			out.WriteString(punct.Render(string(c)))
			i++
		case c == '"':
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			k := j
			for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			token := s[i:j]
			if k < len(s) && s[k] == ':' {
				out.WriteString(key.Render(token))
			} else {
				out.WriteString(str.Render(token))
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(s) {
				cc := s[j]
				if cc == '.' || cc == 'e' || cc == 'E' || cc == '+' || cc == '-' || (cc >= '0' && cc <= '9') {
					j++
					continue
				}
				break
			}
			out.WriteString(num.Render(s[i:j]))
			i = j
		case strings.HasPrefix(s[i:], "true"):
			out.WriteString(kw.Render("true"))
			i += 4
		case strings.HasPrefix(s[i:], "false"):
			out.WriteString(kw.Render("false"))
			i += 5
		case strings.HasPrefix(s[i:], "null"):
			out.WriteString(kw.Render("null"))
			i += 4
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// ----------------------------------------------------------------------
// Event activation registry — maps (resource type, activationID) to a
// handler that transitions the TUI into a sub-view. Used by the
// Details mode when Enter fires on a Selectable event row.
// ----------------------------------------------------------------------

type eventActivator func(m Model, activationID string) (Model, tea.Cmd)

var eventActivationRegistry = map[core.ResourceType]eventActivator{
	core.RTypeSSMAutomationDocument: func(m Model, id string) (Model, tea.Cmd) {
		return enterExecutionDetails(m, id)
	},
}

// selectableEventRows returns the subset of the current details
// resource's Events-zone rows that are Selectable. The Details key
// handler uses this list to implement Tab-focus + Up/Down + Enter
// over a runbook's execution history. Returns nil when the resource
// has no provider, no lazy data, or no selectable event rows.
func selectableEventRows(m Model) []services.DetailRow {
	p, ok := services.Get(m.detailsResource.Type)
	if !ok {
		return nil
	}
	lazy := m.lazyDetailsFor(m.detailsResource)
	rows := p.DetailRows(m.detailsResource, lazy)
	var out []services.DetailRow
	for _, row := range rows {
		if row.Zone == services.ZoneEvents && row.Selectable {
			out = append(out, row)
		}
	}
	return out
}
