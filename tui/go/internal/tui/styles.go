package tui

import "github.com/charmbracelet/lipgloss"

var (
	// State colors.
	colorRunning = lipgloss.Color("#22c55e") // green
	colorWaiting = lipgloss.Color("#eab308") // yellow
	colorIdle    = lipgloss.Color("#9ca3af") // gray
	colorDead    = lipgloss.Color("#4b5563") // dark gray
	colorDestructive = lipgloss.Color("#ef4444") // red
	colorOrange  = lipgloss.Color("#f97316") // orange

	// General UI colors.
	colorFg       = lipgloss.Color("#e5e7eb")
	colorDimFg    = lipgloss.Color("#6b7280")
	colorBorder   = lipgloss.Color("#374151")
	colorAccent   = lipgloss.Color("#3b82f6") // blue
	colorBg       = lipgloss.Color("#111827")
	colorPanelBg  = lipgloss.Color("#1f2937")

	// Styles.
	styleApp = lipgloss.NewStyle().
			Background(colorBg)

	styleZoomPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2).
			Background(colorPanelBg)

	styleZoomHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg)

	styleZoomBranch = lipgloss.NewStyle().
			Foreground(colorDimFg)

	styleActivityLine = lipgloss.NewStyle().
			Foreground(colorDimFg)

	styleMotivation = lipgloss.NewStyle().
			Foreground(colorFg).
			Italic(true)

	styleHintsBar = lipgloss.NewStyle().
			Foreground(colorDimFg).
			Padding(0, 1)

	styleHintKey = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg)

	styleStripBar = lipgloss.NewStyle().
			Padding(0, 1)

	styleQueuePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOrange).
			Padding(1, 2).
			Background(colorPanelBg)

	styleQueueTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorOrange)

	styleSafe = lipgloss.NewStyle().
			Foreground(colorRunning)

	styleDestructive = lipgloss.NewStyle().
			Foreground(colorDestructive)

	styleUnknown = lipgloss.NewStyle().
			Foreground(colorDimFg)

	styleStatusConnected = lipgloss.NewStyle().
			Foreground(colorRunning)

	styleStatusDisconnected = lipgloss.NewStyle().
			Foreground(colorDestructive)
)

// stateColor returns the lipgloss color for a session state.
func stateColor(state string) lipgloss.Color {
	switch state {
	case "running":
		return colorRunning
	case "waiting":
		return colorWaiting
	case "idle":
		return colorIdle
	case "dead":
		return colorDead
	default:
		return colorDimFg
	}
}

// stateIcon returns the icon for a session state.
func stateIcon(state string) string {
	switch state {
	case "running":
		return "\u2699" // gear
	case "waiting":
		return "\u23f3" // hourglass
	case "idle":
		return "\u2713" // checkmark
	case "dead":
		return "\u25cb" // circle
	default:
		return "?"
	}
}
