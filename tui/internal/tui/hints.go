package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHints renders the keyboard hints bar with styled key badges.
func renderHints(queueVisible bool, hasPending bool, width int) string {
	keys := []struct {
		key  string
		desc string
	}{
		{"\u2190\u2192", "navigate"},
		{"Enter", "focus"},
		{"a", "autopilot"},
	}

	if hasPending {
		keys = append(keys,
			struct{ key, desc string }{"y", "approve"},
			struct{ key, desc string }{"n", "reject"},
		)
	}

	if queueVisible {
		keys = append(keys, struct{ key, desc string }{"Esc", "close queue"})
	} else {
		keys = append(keys, struct{ key, desc string }{"Q", "queue"})
	}

	if hasPending {
		keys = append(keys, struct{ key, desc string }{"A", "approve all safe"})
	}

	keys = append(keys,
		struct{ key, desc string }{"h", "help"},
		struct{ key, desc string }{"q", "quit"},
	)

	sep := styleHintSep.Render(" \u2502 ")

	line := ""
	for i, k := range keys {
		if i > 0 {
			line += sep
		}
		line += styleHintKey.Render(k.key) + " " + k.desc
	}

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
