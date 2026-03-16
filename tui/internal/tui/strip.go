package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// renderStrip renders the horizontal session pill strip at the bottom (sessions only).
func renderStrip(sessions []client.Session, selectedIdx int, width int, glowPos int) string {
	return renderUnifiedStrip(sessions, nil, selectedIdx, width, glowPos)
}

// renderUnifiedStrip renders sessions + PRs in one strip with a separator.
func renderUnifiedStrip(sessions []client.Session, prs []client.TrackedPR, selectedIdx int, width int, glowPos int) string {
	if len(sessions) == 0 && len(prs) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(colorDimFg).Italic(true)
		return styleStripBar.Width(width).Render(
			emptyStyle.Render("  No active sessions or PRs"))
	}

	var pills []string

	// Session pills.
	for i, s := range sessions {
		pills = append(pills, renderPill(s, i == selectedIdx, glowPos))
	}

	// Separator between sessions and PRs.
	if len(sessions) > 0 && len(prs) > 0 {
		pills = append(pills, lipgloss.NewStyle().Foreground(colorBorder).Render("│"))
	}

	// PR pills.
	for i, p := range prs {
		prIdx := len(sessions) + i
		pills = append(pills, renderPRPill(p, prIdx == selectedIdx))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Center, interleave(pills, " ")...)
	return styleStripBar.Width(width).Render(row)
}

// renderPRPill renders a single PR pill in the strip.
func renderPRPill(p client.TrackedPR, selected bool) string {
	icon := prPillIcon(p.State)
	label := fmt.Sprintf("%s #%d %s", icon, p.Number, truncateMiddle(p.Title, 15))

	sc := prStateColor(p.State)

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(sc)

	if selected {
		style = style.
			Bold(true).
			Foreground(lipgloss.ANSIColor(15)).
			Background(sc).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(sc)
	}

	return style.Render(label)
}

func prPillIcon(state string) string {
	switch state {
	case "checks_failing":
		return "✗"
	case "checks_running":
		return "⏳"
	case "checks_passing":
		return "✓"
	case "approved":
		return "✅"
	case "merged":
		return "🚀"
	default:
		return "•"
	}
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
	if maxLen <= 0 {
		return ""
	}
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
