package tui

import "github.com/charmbracelet/lipgloss"

// All colors use ANSI 0-15 palette so they adapt to the terminal's theme.
var (
	// ── State colors (from terminal palette) ────────────────────
	colorRunning     = lipgloss.ANSIColor(2)  // green
	colorRunningDim  = lipgloss.ANSIColor(22) // dark green (256-color)
	colorWaiting     = lipgloss.ANSIColor(3)  // yellow
	colorWaitingDim  = lipgloss.ANSIColor(58) // dark yellow (256-color)
	colorIdle        = lipgloss.ANSIColor(8)  // bright black / gray
	colorIdleDim     = lipgloss.ANSIColor(0)  // black
	colorDead        = lipgloss.ANSIColor(8)  // bright black / gray
	colorDeadDim     = lipgloss.ANSIColor(0)  // black
	colorDestructive = lipgloss.ANSIColor(1)  // red
	colorOrange      = lipgloss.ANSIColor(3)  // yellow (closest ANSI)

	// ── UI chrome ────────────────────────────────────────────────
	colorFg        = lipgloss.ANSIColor(15) // bright white
	colorDimFg     = lipgloss.ANSIColor(8)  // bright black / gray
	colorSubtle    = lipgloss.ANSIColor(8)  // bright black / gray
	colorBorder    = lipgloss.ANSIColor(8)  // bright black / gray
	colorAccent    = lipgloss.ANSIColor(4)  // blue
	colorAccentDim = lipgloss.ANSIColor(0)  // black
	colorBg        = lipgloss.ANSIColor(0)  // black (terminal bg)
	colorPanelBg   = lipgloss.ANSIColor(0)  // black
	colorCardBg    = lipgloss.ANSIColor(0)  // black
	colorBadgeBg   = lipgloss.ANSIColor(5)  // magenta

	// ── Section label color ──────────────────────────────────────
	colorLabel = lipgloss.ANSIColor(12) // bright blue

	// ── Zoom Panel ───────────────────────────────────────────────
	styleZoomPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

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

	// ── Hints bar ────────────────────────────────────────────────
	styleHintsBar = lipgloss.NewStyle().
			Foreground(colorDimFg).
			Padding(0, 1)

	styleHintKey = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg).
			Background(colorAccentDim).
			Padding(0, 1)

	styleHintSep = lipgloss.NewStyle().
			Foreground(colorSubtle)

	// ── Strip bar ────────────────────────────────────────────────
	styleStripBar = lipgloss.NewStyle().
			Padding(0, 1).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	// ── Queue panel ──────────────────────────────────────────────
	styleQueuePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOrange).
			Padding(1, 2)

	styleQueueTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorOrange)

	// ── Safety markers ───────────────────────────────────────────
	styleSafe = lipgloss.NewStyle().
			Foreground(colorRunning)

	styleDestructive = lipgloss.NewStyle().
			Foreground(colorDestructive)

	styleUnknown = lipgloss.NewStyle().
			Foreground(colorDimFg)

	// ── Connection status ────────────────────────────────────────
	styleStatusConnected = lipgloss.NewStyle().
				Foreground(colorRunning)

	styleStatusDisconnected = lipgloss.NewStyle().
				Foreground(colorDestructive)

	// ── Section labels (Activities:, Pending:, etc.) ─────────────
	styleSectionLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorLabel)

	// ── Autopilot badge ──────────────────────────────────────────
	styleAutopilotOn = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.ANSIColor(0)).
			Background(colorRunning).
			Padding(0, 1)

	styleAutopilotWarn = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.ANSIColor(0)).
				Background(colorOrange).
				Padding(0, 1)
)

// stateColor returns the color for a session state.
func stateColor(state string) lipgloss.TerminalColor {
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

// stateColorDim returns a muted background shade for a session state.
func stateColorDim(state string) lipgloss.TerminalColor {
	switch state {
	case "running":
		return colorRunningDim
	case "waiting":
		return colorWaitingDim
	case "idle":
		return colorIdleDim
	case "dead":
		return colorDeadDim
	default:
		return colorBorder
	}
}

// stateIcon returns a distinctive icon for a session state.
func stateIcon(state string) string {
	switch state {
	case "running":
		return "\u25b6" // ▶  play/running
	case "waiting":
		return "\u23f8" // ⏸  pause/waiting
	case "idle":
		return "\u2714" // ✔  checkmark
	case "dead":
		return "\u25cf" // ●  filled circle (stopped)
	default:
		return "\u2022" // •  bullet
	}
}

// stateLabel returns a human-friendly label for a state.
func stateLabel(state string) string {
	switch state {
	case "running":
		return "RUNNING"
	case "waiting":
		return "WAITING"
	case "idle":
		return "IDLE"
	case "dead":
		return "STOPPED"
	default:
		return "UNKNOWN"
	}
}
