package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHints renders the keyboard hints bar, fitting as many hints as the
// width allows.  The last hint is always "h help" so the user knows how to
// discover the rest.
func renderHints(queueVisible bool, hasPending bool, width int) string {
	type hint struct {
		key  string
		desc string
	}

	// Build ordered hint list — "h help" is always last.
	var keys []hint
	keys = append(keys, hint{"\u2190\u2192\u2191\u2193", "navigate"})
	keys = append(keys, hint{"Enter", "focus"})
	keys = append(keys, hint{"a", "autopilot"})

	if hasPending {
		keys = append(keys, hint{"y", "approve"}, hint{"n", "reject"})
	}

	if queueVisible {
		keys = append(keys, hint{"Esc", "close queue"})
	} else {
		keys = append(keys, hint{"Q", "queue"})
	}

	if hasPending {
		keys = append(keys, hint{"A", "approve all"})
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

// renderHelp renders a full-screen help overlay with keybindings and info.
func renderHelp(width, height int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		Render("CSM Help")

	sep := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(width-6, 50)))

	bindings := []struct{ key, desc string }{
		{"\u2190 / \u2192", "Navigate between sessions"},
		{"\u2191 / \u2193", "Scroll detail panel"},
		{"Home / End", "Jump to first / last session"},
		{"PgUp / PgDn", "Scroll 5 lines at a time"},
		{"Enter", "Focus (switch to) the selected session's Ghostty tab"},
		{"a", "Toggle autopilot for the selected session"},
		{"y", "Approve the pending tool call"},
		{"n", "Reject the pending tool call"},
		{"Q", "Toggle approval queue overlay"},
		{"A", "Approve all safe pending tool calls"},
		{"h", "Toggle this help screen"},
		{"Esc", "Close help / close queue"},
		{"q", "Quit CSM"},
	}

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorFg).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(colorDimFg)

	var lines []string
	for _, b := range bindings {
		lines = append(lines, "  "+keyStyle.Render(b.key)+"  "+descStyle.Render(b.desc))
	}

	autopilotInfo := lipgloss.NewStyle().Foreground(colorDimFg).Italic(true).Render(
		"Autopilot auto-approves safe tools. Destructive commands always need manual approval.")

	stateInfo := lipgloss.NewStyle().Foreground(colorDimFg).Render(
		"\u25b6 running  \u23f8 waiting  \u2714 idle  \u25cf stopped")

	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		sep,
		"",
		strings.Join(lines, "\n"),
		"",
		sep,
		"",
		autopilotInfo,
		"",
		stateInfo,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 3).
		Background(colorPanelBg).
		Width(width - 4).
		Height(height - 2).
		Render(body)
}
