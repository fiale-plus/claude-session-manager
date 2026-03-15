package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// renderZoom renders the session detail panel, hard-clipped to height.
// Layout: fixed header (4-5 lines) + scrollable body (activities, pending, last output).
func renderZoom(s client.Session, width, height int) string {
	if width < 10 || height < 4 {
		return ""
	}

	innerWidth := width - 4 // padding only, no border

	// ── Build all lines, then clip to height ──────────────────

	var lines []string

	// Header: name + state badge + branch (always visible)
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.ANSIColor(0)).
		Background(stateColor(s.State)).
		Bold(true).
		Padding(0, 1)

	headerLine := styleZoomHeader.Render(pillName(s)) +
		" " + stateStyle.Render(stateLabel(s.State))
	if s.GitBranch != "" {
		headerLine += "  " + lipgloss.NewStyle().Foreground(colorAccent).
			Render("\ue0a0 "+s.GitBranch)
	}
	lines = append(lines, headerLine)

	// Autopilot badge
	if s.Autopilot {
		apStyle := styleAutopilotOn
		if s.HasDestructive {
			apStyle = styleAutopilotWarn
		}
		apLine := apStyle.Render("\u2699 AUTOPILOT")
		if s.HasDestructive {
			apLine += "  " + styleDestructive.Render("\u26a0 destructive pending")
		}
		lines = append(lines, apLine)
	}

	// PID + CWD + last activity on one line
	infoLine := lipgloss.NewStyle().Foreground(colorDimFg).Render(
		fmt.Sprintf("PID %d", s.PID))
	infoLine += "  " + lipgloss.NewStyle().Foreground(colorSubtle).Render(
		truncateMiddle(s.CWD, innerWidth-20))
	if s.LastActivity != nil {
		ago := time.Since(*s.LastActivity).Truncate(time.Second)
		infoLine += "  " + lipgloss.NewStyle().Foreground(colorDimFg).Render(
			fmt.Sprintf("\u23f1 %s ago", ago))
	}
	lines = append(lines, infoLine)

	// Thin separator
	sep := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(innerWidth, 60)))

	// ── Pending approval (priority — always show if present) ──
	if len(s.PendingTools) > 0 {
		lines = append(lines, sep)
		countBadge := lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(15)).
			Background(colorOrange).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%d", len(s.PendingTools)))
		lines = append(lines,
			styleSectionLabel.Foreground(colorOrange).
				Render("\u2500\u2500 Pending Approval")+" "+countBadge)
		for _, pt := range s.PendingTools {
			marker := safetyMarker(pt.Safety)
			detail := toolDetail(pt, innerWidth-20)
			toolLabel := pt.ToolName
			if detail != "" {
				toolLabel += ": " + detail
			}
			lines = append(lines, fmt.Sprintf("  %s %s", marker,
				lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(toolLabel)))
		}
	}

	// ── Activities (clip to fit) ─────────────────────────────
	if len(s.Activities) > 0 {
		lines = append(lines, sep)
		lines = append(lines, styleSectionLabel.Render("\u2500\u2500 Activities"))

		// Show as many as fit — newest first priority
		start := 0
		maxActivities := height - len(lines) - 4 // leave room for last output
		if maxActivities < 2 {
			maxActivities = 2
		}
		if len(s.Activities) > maxActivities {
			start = len(s.Activities) - maxActivities
		}
		visible := s.Activities[start:]
		total := len(visible)
		for idx, a := range visible {
			age := total - 1 - idx
			tsColor := lipgloss.TerminalColor(colorSubtle)
			sumColor := lipgloss.TerminalColor(colorDimFg)
			if age >= 4 {
				tsColor = colorBorder
				sumColor = colorBorder
			} else if age >= 2 {
				tsColor = colorBorder
				sumColor = colorSubtle
			}
			ts := lipgloss.NewStyle().Foreground(tsColor).
				Render(a.Timestamp.Format("15:04:05"))
			icon := lipgloss.NewStyle().Foreground(activityColor(a.ActivityType)).
				Render(activityIcon(a.ActivityType))
			summary := lipgloss.NewStyle().Foreground(sumColor).
				Render(truncateMiddle(a.Summary, innerWidth-20))
			lines = append(lines, fmt.Sprintf("  %s  %s  %s", ts, icon, summary))
		}
	}

	// ── Last Output (fill remaining space) ───────────────────
	if s.LastText != "" {
		remaining := height - len(lines) - 1
		if remaining >= 2 {
			lines = append(lines, sep)
			lines = append(lines, styleSectionLabel.Render("\u2500\u2500 Last Output"))
			remaining -= 2

			text := s.LastText
			// Wrap to width, then clip to remaining lines
			wrapped := lipgloss.NewStyle().Width(innerWidth).Render("  \u201c" + text + "\u201d")
			wrappedLines := strings.Split(wrapped, "\n")
			if len(wrappedLines) > remaining {
				wrappedLines = wrappedLines[:remaining]
				// Add ellipsis to last line
				if len(wrappedLines) > 0 {
					wrappedLines[len(wrappedLines)-1] += "\u2026"
				}
			}
			for _, wl := range wrappedLines {
				lines = append(lines, styleMotivation.Render(wl))
			}
		}
	}

	// ── Hard clip to height ─────────────────────────────────
	if len(lines) > height {
		lines = lines[:height]
	}

	body := strings.Join(lines, "\n")

	// No heavy border — just padding and width constraint.
	return lipgloss.NewStyle().
		Padding(0, 2).
		Width(width).
		Height(height).
		Render(body)
}

func activityIcon(actType string) string {
	switch actType {
	case "tool_use":
		return "\u2699" // gear
	case "text":
		return "\u270e" // pencil
	case "thinking":
		return "\U0001f4ad" // thought bubble
	case "user_message":
		return "\u25b6" // play
	case "system":
		return "\u26a0" // warning
	default:
		return "\u2022" // bullet
	}
}

func activityColor(actType string) lipgloss.TerminalColor {
	switch actType {
	case "tool_use":
		return colorAccent
	case "text":
		return colorRunning
	case "thinking":
		return colorWaiting
	case "user_message":
		return lipgloss.ANSIColor(6) // cyan
	case "system":
		return colorOrange
	default:
		return colorDimFg
	}
}

func toolDetail(pt client.PendingTool, maxLen int) string {
	if pt.ToolInput == nil {
		return ""
	}
	for _, key := range []string{"command", "file_path", "pattern", "query", "description", "prompt"} {
		if v, ok := pt.ToolInput[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > maxLen && maxLen > 5 {
				s = s[:maxLen-3] + "..."
			}
			return s
		}
	}
	return ""
}

func safetyMarker(safety string) string {
	switch safety {
	case "safe":
		return styleSafe.Render("\u2713")
	case "destructive":
		return styleDestructive.Render("\u26a0")
	default:
		return styleUnknown.Render("\u2022")
	}
}
