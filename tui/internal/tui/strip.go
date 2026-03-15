package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// renderStrip renders the horizontal session pill strip at the bottom.
func renderStrip(sessions []client.Session, selectedIdx int, width int, glowPos int) string {
	if len(sessions) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(colorDimFg).
			Italic(true)
		return styleStripBar.Width(width).Render(
			emptyStyle.Render("  No active sessions"))
	}

	var pills []string
	for i, s := range sessions {
		pills = append(pills, renderPill(s, i == selectedIdx, glowPos))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Center, interleave(pills, " ")...)

	return styleStripBar.Width(width).Render(row)
}

// interleave inserts a separator between each element.
func interleave(items []string, sep string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}

// truncateMiddle truncates a string in the middle if it exceeds maxLen.
func truncateMiddle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen < 5 {
		return string(runes[:maxLen])
	}
	half := (maxLen - 3) / 2
	return string(runes[:half]) + "\u2026" + string(runes[len(runes)-half:])
}

// countPending returns the total pending tools across all sessions.
func countPending(sessions []client.Session) int {
	n := 0
	for _, s := range sessions {
		n += len(s.PendingTools)
	}
	return n
}

// padRight pads a string with spaces to the given width.
func padRight(s string, w int) string {
	runes := []rune(s)
	if len(runes) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(runes))
}
