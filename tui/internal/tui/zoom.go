package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// renderZoom renders the session detail panel with fixed header + scrollable body.
func renderZoom(s client.Session, width, height int, scrollOffset int) string {
	if width < 10 || height < 4 {
		return ""
	}

	innerWidth := width - 4

	// ═══════════════════════════════════════════════════════════
	// FIXED HEADER — always visible, pinned below status bar
	// ═══════════════════════════════════════════════════════════
	var headerLines []string

	// Line 1: name + state badge + branch
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.ANSIColor(0)).
		Background(stateColor(s.State)).
		Bold(true).
		Padding(0, 1)

	line1 := "  " + styleZoomHeader.Render(pillName(s)) +
		" " + stateStyle.Render(stateLabel(s.State))
	if s.GitBranch != "" {
		line1 += "  " + lipgloss.NewStyle().Foreground(colorAccent).
			Render("\ue0a0 "+s.GitBranch)
	}
	headerLines = append(headerLines, line1)

	// Line 2: autopilot badge (if on)
	if s.Autopilot {
		apStyle := styleAutopilotOn
		if s.HasDestructive {
			apStyle = styleAutopilotWarn
		}
		apLine := "   " + apStyle.Render("\u2699 AUTOPILOT")
		if s.HasDestructive {
			apLine += "  " + styleDestructive.Render("\u26a0 destructive pending")
		}
		headerLines = append(headerLines, apLine)
	}

	// Line 3: PID + CWD + last activity
	infoLine := "  " + lipgloss.NewStyle().Foreground(colorDimFg).Render(
		fmt.Sprintf("PID %d", s.PID))
	infoLine += "  " + lipgloss.NewStyle().Foreground(colorSubtle).Render(
		truncateMiddle(s.CWD, innerWidth-30))
	if s.LastActivity != nil {
		ago := time.Since(*s.LastActivity).Truncate(time.Second)
		infoLine += "  " + lipgloss.NewStyle().Foreground(colorDimFg).Render(
			fmt.Sprintf("\u23f1 %s ago", ago))
	}
	headerLines = append(headerLines, infoLine)

	headerHeight := len(headerLines)
	bodyHeight := height - headerHeight

	// ═══════════════════════════════════════════════════════════
	// SCROLLABLE BODY — activities, pending, last output
	// ═══════════════════════════════════════════════════════════
	var bodyLines []string

	sep := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(innerWidth, 60)))

	// Pending approval
	if len(s.PendingTools) > 0 {
		countBadge := lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(15)).
			Background(colorOrange).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%d", len(s.PendingTools)))
		bodyLines = append(bodyLines,
			styleSectionLabel.Foreground(colorOrange).
				Render("\u2500\u2500 Pending Approval")+" "+countBadge)
		for _, pt := range s.PendingTools {
			marker := safetyMarker(pt.Safety)
			detail := toolDetail(pt, innerWidth-20)
			toolLabel := pt.ToolName
			if detail != "" {
				toolLabel += ": " + detail
			}
			bodyLines = append(bodyLines, fmt.Sprintf("  %s %s", marker,
				lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(toolLabel)))
		}
		bodyLines = append(bodyLines, sep)
	}

	// Activities
	if len(s.Activities) > 0 {
		bodyLines = append(bodyLines, styleSectionLabel.Render("\u2500\u2500 Activities"))
		start := 0
		if len(s.Activities) > 8 {
			start = len(s.Activities) - 8
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
			bodyLines = append(bodyLines, fmt.Sprintf("  %s  %s  %s", ts, icon, summary))
		}
	}

	// Last Output
	if s.LastText != "" {
		bodyLines = append(bodyLines, sep)
		bodyLines = append(bodyLines, styleSectionLabel.Render("\u2500\u2500 Last Output"))
		text := s.LastText
		wrapped := lipgloss.NewStyle().Width(innerWidth).Render("  \u201c" + text + "\u201d")
		for _, wl := range strings.Split(wrapped, "\n") {
			bodyLines = append(bodyLines, styleMotivation.Render(wl))
		}
	}

	// ═══════════════════════════════════════════════════════════
	// SCROLL + CLIP
	// ═══════════════════════════════════════════════════════════

	// Clamp scroll offset
	maxScroll := len(bodyLines) - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}

	// Apply scroll
	visibleBody := bodyLines
	if scrollOffset > 0 && scrollOffset < len(visibleBody) {
		visibleBody = visibleBody[scrollOffset:]
	}
	// Clip to body height
	if len(visibleBody) > bodyHeight {
		visibleBody = visibleBody[:bodyHeight]
	}

	// Scroll indicator
	scrollInfo := ""
	if maxScroll > 0 {
		pct := 0
		if maxScroll > 0 {
			pct = scrollOffset * 100 / maxScroll
		}
		if scrollOffset > 0 {
			scrollInfo = lipgloss.NewStyle().Foreground(colorDimFg).
				Render(fmt.Sprintf(" \u2191\u2193 %d%%", pct))
		} else {
			scrollInfo = lipgloss.NewStyle().Foreground(colorDimFg).
				Render(" \u2193 scroll")
		}
		// Append to last header line
		headerLines[len(headerLines)-1] += scrollInfo
	}

	// ═══════════════════════════════════════════════════════════
	// ASSEMBLE
	// ═══════════════════════════════════════════════════════════
	all := append(headerLines, visibleBody...)

	// Pad to full height
	for len(all) < height {
		all = append(all, "")
	}

	body := strings.Join(all, "\n")
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(body)
}

func activityIcon(actType string) string {
	switch actType {
	case "tool_use":
		return "\u2699"
	case "text":
		return "\u270e"
	case "thinking":
		return "\U0001f4ad"
	case "user_message":
		return "\u25b6"
	case "system":
		return "\u26a0"
	default:
		return "\u2022"
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
		return lipgloss.ANSIColor(6)
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
