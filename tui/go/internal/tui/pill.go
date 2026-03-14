package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// looksLikeCommand returns true if the string looks like a shell command
// rather than a meaningful session name.
func looksLikeCommand(s string) bool {
	for _, marker := range []string{"&&", "|", ";", "cd ", "./", "  "} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	if strings.HasPrefix(s, "/") {
		return true
	}
	return false
}

// pillName picks the best display name for a session:
// ghostty_tab (if not command-like) > slug > project_name > session_id[:8]
func pillName(s client.Session) string {
	if s.GhosttyTab != "" && !looksLikeCommand(s.GhosttyTab) {
		return s.GhosttyTab
	}
	if s.Slug != "" {
		return s.Slug
	}
	if s.ProjectName != "" {
		return s.ProjectName
	}
	if len(s.SessionID) >= 8 {
		return s.SessionID[:8]
	}
	return s.SessionID
}

// renderPill renders a single session pill with state-colored background,
// icon, name, and optional pending-tool count badge.
func renderPill(s client.Session, selected bool, glowPos int) string {
	sc := stateColor(s.State)
	dimBg := stateColorDim(s.State)
	icon := stateIcon(s.State)

	name := pillName(s)

	label := icon + " " + name

	// Pending badge.
	if n := len(s.PendingTools); n > 0 {
		badgeStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(colorBadgeBg).
			Bold(true).
			Padding(0, 1)
		label += " " + badgeStyle.Render(fmt.Sprintf("%d", n))
	}

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(sc).
		Background(dimBg)

	if selected {
		accentColor := colorAccent
		if s.Autopilot && s.HasDestructive {
			accentColor = colorOrange
		} else if s.Autopilot {
			accentColor = colorRunning
		}
		style = style.
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(sc).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor)
	}
	// Unselected pills: no border at all.

	return style.Render(label)
}
