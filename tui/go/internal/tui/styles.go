package tui

import "github.com/charmbracelet/lipgloss"

var (
	// ── State colors ─────────────────────────────────────────────
	colorRunning     = lipgloss.Color("#22c55e") // vibrant green
	colorRunningDim  = lipgloss.Color("#166534") // muted green for bg
	colorWaiting     = lipgloss.Color("#facc15") // bright yellow
	colorWaitingDim  = lipgloss.Color("#854d0e") // muted amber
	colorIdle        = lipgloss.Color("#94a3b8") // slate gray
	colorIdleDim     = lipgloss.Color("#334155") // dark slate
	colorDead        = lipgloss.Color("#64748b") // cool gray
	colorDeadDim     = lipgloss.Color("#1e293b") // dark cool gray
	colorDestructive = lipgloss.Color("#ef4444") // red
	colorOrange      = lipgloss.Color("#f97316") // orange

	// ── UI chrome ────────────────────────────────────────────────
	colorFg        = lipgloss.Color("#f1f5f9") // near-white slate
	colorDimFg     = lipgloss.Color("#64748b") // muted text
	colorSubtle    = lipgloss.Color("#475569") // subtle separators
	colorBorder    = lipgloss.Color("#334155") // panel borders
	colorAccent    = lipgloss.Color("#6366f1") // indigo accent
	colorAccentDim = lipgloss.Color("#312e81") // dark indigo
	colorBg        = lipgloss.Color("#0f172a") // deep navy bg
	colorPanelBg   = lipgloss.Color("#1e293b") // panel bg
	colorCardBg    = lipgloss.Color("#1e293b") // card bg
	colorBadgeBg   = lipgloss.Color("#7c3aed") // purple badge

	// ── Section label color ──────────────────────────────────────
	colorLabel = lipgloss.Color("#a5b4fc") // light indigo for labels

	// ── Zoom Panel ───────────────────────────────────────────────
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
			Padding(1, 2).
			Background(colorPanelBg)

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
			Foreground(lipgloss.Color("#000000")).
			Background(colorRunning).
			Padding(0, 1)

	styleAutopilotWarn = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(colorOrange).
				Padding(0, 1)
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

// stateColorDim returns a muted background shade for a session state.
func stateColorDim(state string) lipgloss.Color {
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
