package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// renderPill renders a single session pill with state-colored background,
// icon, name, and optional pending-tool count badge.
func renderPill(s client.Session, selected bool) string {
	sc := stateColor(s.State)
	dimBg := stateColorDim(s.State)
	icon := stateIcon(s.State)

	name := s.ProjectName
	if name == "" {
		name = s.SessionID[:min(8, len(s.SessionID))]
	}

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
		style = style.
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(sc).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent)
	}

	// Autopilot border accent (non-selected).
	if s.Autopilot && !selected {
		borderColor := colorRunning
		if s.HasDestructive {
			borderColor = colorOrange
		}
		style = style.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor)
	}

	return style.Render(label)
}
