package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// renderOnboarding produces the full-body content for modeOnboarding.
// The content branches on whether AWS profiles exist: with profiles, we
// invite the user to open the switcher; without, we walk them through
// the minimum setup steps so they don't have to leave the TUI to fix
// things. The resolve-error message is shown verbatim so power users
// who recognise a specific failure (expired SSO, missing region, etc.)
// can act on it immediately.
func renderOnboarding(m Model, width, height int) string {
	hasProfiles := len(m.onboardingProfiles) > 0

	title := styleOnboardingTitle.Render("Welcome to scout")
	subtitle := styleOnboardingSubtitle.Render("AWS credentials couldn't be resolved for this session.")

	reasonBlock := ""
	if m.onboardingReason != "" {
		reasonBlock = styleOnboardingReason.Render("reason: " + m.onboardingReason)
	}

	var body string
	if hasProfiles {
		profileList := strings.Join(m.onboardingProfiles, ", ")
		if lipgloss.Width(profileList) > width-8 {
			profileList = fmt.Sprintf("%d profiles found", len(m.onboardingProfiles))
		}
		body = strings.Join([]string{
			"We found configured profiles: " + styleOnboardingHighlight.Render(profileList),
			"",
			"Press " + styleOnboardingKey.Render("Ctrl+P") + " to pick a profile and region.",
			"",
			styleRowDim.Render("Ctrl+C quits."),
		}, "\n")
	} else {
		body = strings.Join([]string{
			"No AWS profiles were found in " + styleOnboardingHighlight.Render("~/.aws/config") + " or " + styleOnboardingHighlight.Render("~/.aws/credentials") + ".",
			"",
			styleOnboardingStep.Render("1. Install the AWS CLI"),
			"   " + styleOnboardingHighlight.Render("brew install awscli") + "  (or see https://aws.amazon.com/cli/)",
			"",
			styleOnboardingStep.Render("2. Configure a profile"),
			"   " + styleOnboardingHighlight.Render("aws configure --profile myprofile"),
			"",
			styleOnboardingStep.Render("3. Restart scout"),
			"   You can also set " + styleOnboardingHighlight.Render("AWS_ACCESS_KEY_ID") + " / " + styleOnboardingHighlight.Render("AWS_SECRET_ACCESS_KEY") + " / " + styleOnboardingHighlight.Render("AWS_REGION") + " directly.",
			"",
			styleRowDim.Render("Ctrl+P opens the switcher once profiles are set up. Ctrl+C quits."),
		}, "\n")
	}

	content := strings.Join([]string{title, subtitle, "", reasonBlock, "", body}, "\n")
	placed := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	return placed
}

// updateOnboarding handles key events while on the onboarding screen.
// Ctrl+C quits; Ctrl+P opens the profile/region switcher (reusing the
// existing switcher path). Every other key is a no-op — this screen is
// deliberately quiet so the user's eye stays on the instructions.
func (m Model) updateOnboarding(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+p":
		m.switcher = newSwitcher(m.awsCtx.Profile, m.awsCtx.Region)
		m.switcher.Show()
		m.prevMode = modeOnboarding
		m.mode = modeSwitcher
		return m, nil
	}
	return m, nil
}

var (
	styleOnboardingTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ac("#005FAF", "#5FD7FF"))
	styleOnboardingSubtitle = lipgloss.NewStyle().
				Foreground(ac("#767676", "#8A8A8A"))
	styleOnboardingReason = lipgloss.NewStyle().
				Foreground(ac("#870000", "#FF8787")).
				Italic(true)
	styleOnboardingStep = lipgloss.NewStyle().
				Bold(true).
				Foreground(ac("#000000", "#FFFFFF"))
	styleOnboardingHighlight = lipgloss.NewStyle().
					Bold(true).
					Foreground(ac("#005FAF", "#5FD7FF"))
	styleOnboardingKey = lipgloss.NewStyle().
				Bold(true).
				Foreground(ac("#AF8700", "#FFD75F"))
)
