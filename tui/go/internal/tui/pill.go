package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// renderPill renders a single session pill like [icon project_name].
func renderPill(s client.Session, selected bool) string {
	sc := stateColor(s.State)
	icon := stateIcon(s.State)

	name := s.ProjectName
	if name == "" {
		name = s.SessionID[:min(8, len(s.SessionID))]
	}

	label := icon + " " + name

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Background(sc).
		Foreground(lipgloss.Color("#000000"))

	if selected {
		style = style.
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent)
	}

	// Autopilot border accent.
	if s.Autopilot && !selected {
		if s.HasDestructive {
			style = style.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorOrange)
		} else {
			style = style.
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorRunning)
		}
	}

	return style.Render(label)
}
