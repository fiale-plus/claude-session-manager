package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHints renders the keyboard hints bar, fitting as many hints as the
// width allows.  The last hint is always "h help" so the user knows how to
// discover the rest.
func renderHints(queueVisible bool, hasPending bool, isPRSelected bool, width int) string {
	type hint struct {
		key  string
		desc string
	}

	var keys []hint
	keys = append(keys, hint{"\u2190\u2192\u2191\u2193", "navigate"})
	keys = append(keys, hint{"Tab", "next alert"})

	if isPRSelected {
		// PR-specific hints.
		keys = append(keys, hint{"Enter", "open PR"})
		keys = append(keys, hint{"a", "autopilot"})
		keys = append(keys, hint{"r", "review"})
		keys = append(keys, hint{"m", "method"})
		keys = append(keys, hint{"+", "add PR"})
		keys = append(keys, hint{"-", "remove"})
	} else {
		// Session-specific hints.
		keys = append(keys, hint{"Enter", "focus"})
		keys = append(keys, hint{"a", "autopilot"})
		keys = append(keys, hint{"d", "default"})
		if hasPending {
			keys = append(keys, hint{"y", "approve"}, hint{"n", "reject"})
		}
		if queueVisible {
			keys = append(keys, hint{"Esc", "close"})
		} else {
			keys = append(keys, hint{"Q", "queue"})
		}
		if hasPending {
			keys = append(keys, hint{"A", "approve all"})
		}
	}

	// "h help" is the anchor — always shown last.
	anchor := hint{"h", "help"}

	sep := styleHintSep.Render(" \u2502 ")
	sepWidth := lipgloss.Width(sep)

	// Pre-render anchor to know its width.
	anchorRendered := styleHintKey.Render(anchor.key) + " " + anchor.desc
	anchorWidth := lipgloss.Width(anchorRendered)

	// Available width for non-anchor hints (account for padding(0,1) = 2 chars).
	budget := width - 2 - anchorWidth - sepWidth

	var parts []string
	used := 0
	for _, k := range keys {
		part := styleHintKey.Render(k.key) + " " + k.desc
		partWidth := lipgloss.Width(part)
		needed := partWidth
		if len(parts) > 0 {
			needed += sepWidth
		}
		if used+needed > budget {
			break
		}
		parts = append(parts, part)
		used += needed
	}

	parts = append(parts, anchorRendered)

	line := strings.Join(parts, sep)
	return styleHintsBar.Width(width).Render(line)
}

// renderHelp renders a scrollable help overlay.
func renderHelp(width, height, scrollOffset int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		Render("CSM Help")

	sep := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(width-6, 50)))

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	bindings := []struct{ key, desc string }{
		{"\u2190 / \u2192", "Navigate sessions and PRs"},
		{"\u2191 / \u2193", "Scroll detail panel"},
		{"Home / End", "Jump to first / last item"},
		{"PgUp / PgDn", "Scroll 5 lines at a time"},
		{"", ""},
		{"", "Sessions"},
		{"Enter", "Focus — switch to Ghostty tab"},
		{"a", "Cycle autopilot: OFF → ON → YOLO"},
		{"d", "Cycle default autopilot for new sessions"},
		{"y / n", "Approve / reject pending tool"},
		{"A", "Approve all safe pending tools"},
		{"Q", "Toggle approval queue"},
		{"", ""},
		{"", "Pull Requests"},
		{"Enter", "Open PR in browser"},
		{"a", "Cycle PR autopilot: OFF → AUTO → YOLO"},
		{"r", "Toggle code review on/off"},
		{"+", "Add PR to tracking (paste URL)"},
		{"-", "Remove selected PR"},
		{"m", "Set merge method (daemon handles actual merge)"},
		{"o", "Open PR in browser"},
		{"", ""},
		{"h", "Toggle this help screen"},
		{"Esc", "Close overlay"},
		{"q", "Quit"},
	}

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorFg).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(colorDimFg)

	var lines []string
	for _, b := range bindings {
		if b.key == "" && b.desc == "" {
			lines = append(lines, "")
			continue
		}
		if b.key == "" {
			lines = append(lines, "  "+sectionStyle.Render(b.desc))
			continue
		}
		lines = append(lines, "  "+keyStyle.Render(b.key)+"  "+descStyle.Render(b.desc))
	}

	autopilotInfo := lipgloss.NewStyle().Foreground(colorDimFg).Italic(true).Render(
		"Session: OFF → ON (safe auto) → YOLO (all, 10s grace for destructive)\n" +
			"PR: OFF → AUTO (hammer CI + merge on approval) → YOLO (merge without review)\n" +
			"Default autopilot ('d'): applies to new sessions only. Per-session overrides take precedence.")

	stateInfo := lipgloss.NewStyle().Foreground(colorDimFg).Render(
		"Sessions: \u25b6 running  \u23f8 waiting  \u2714 idle  \u25cf stopped\n" +
			"PRs: \u2717 failing  \u2713 passing  \u23f3 running  \U0001F680 merged")

	// Build all content lines.
	var allLines []string
	allLines = append(allLines, title)
	allLines = append(allLines, sep)
	allLines = append(allLines, "")
	allLines = append(allLines, lines...)
	allLines = append(allLines, "")
	allLines = append(allLines, sep)
	allLines = append(allLines, "")
	for _, l := range strings.Split(autopilotInfo, "\n") {
		allLines = append(allLines, l)
	}
	allLines = append(allLines, "")
	for _, l := range strings.Split(stateInfo, "\n") {
		allLines = append(allLines, l)
	}

	// Scroll + clip.
	viewHeight := height - 4
	maxScroll := len(allLines) - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}
	visible := allLines
	if scrollOffset > 0 && scrollOffset < len(visible) {
		visible = visible[scrollOffset:]
	}
	if len(visible) > viewHeight {
		visible = visible[:viewHeight]
	}

	body := strings.Join(visible, "\n")

	return lipgloss.NewStyle().
		Padding(1, 3).
		Width(width).
		Height(height).
		Render(body)
}
