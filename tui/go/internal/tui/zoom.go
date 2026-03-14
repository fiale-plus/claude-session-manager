package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// renderZoom renders the expanded session detail panel.
func renderZoom(s client.Session, width, height int) string {
	if width < 10 || height < 4 {
		return ""
	}

	innerWidth := width - 6 // account for padding + border

	// ── Header: project + state badge + branch ──────────────────
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(stateColor(s.State)).
		Bold(true).
		Padding(0, 1)

	header := styleZoomHeader.Render(s.ProjectName) +
		" " + stateStyle.Render(stateLabel(s.State))

	if s.GitBranch != "" {
		branchPill := lipgloss.NewStyle().
			Foreground(colorAccent).
			Render("\ue0a0 " + s.GitBranch) // git branch icon
		header += "  " + branchPill
	}

	// ── Autopilot indicator ─────────────────────────────────────
	autopilotLine := ""
	if s.Autopilot {
		apStyle := styleAutopilotOn
		if s.HasDestructive {
			apStyle = styleAutopilotWarn
		}
		autopilotLine = apStyle.Render("\u2699 AUTOPILOT")
		if s.HasDestructive {
			autopilotLine += "  " + styleDestructive.Render("\u26a0 destructive pending")
		}
	}

	// ── PID + CWD ───────────────────────────────────────────────
	pidStr := lipgloss.NewStyle().Foreground(colorDimFg).Render(
		fmt.Sprintf("PID %d", s.PID))
	cwdStr := lipgloss.NewStyle().Foreground(colorSubtle).Render(
		truncateMiddle(s.CWD, innerWidth-12))
	info := pidStr + "  " + cwdStr

	// ── Last activity ───────────────────────────────────────────
	lastActivityStr := ""
	if s.LastActivity != nil {
		ago := time.Since(*s.LastActivity).Truncate(time.Second)
		lastActivityStr = lipgloss.NewStyle().
			Foreground(colorDimFg).
			Render(fmt.Sprintf("\u23f1 %s ago", ago)) // stopwatch icon
	}

	// ── Activities timeline (last 6) ────────────────────────────
	activityBlock := ""
	if len(s.Activities) > 0 {
		start := 0
		if len(s.Activities) > 6 {
			start = len(s.Activities) - 6
		}

		var activityLines []string
		for _, a := range s.Activities[start:] {
			ts := lipgloss.NewStyle().
				Foreground(colorSubtle).
				Render(a.Timestamp.Format("15:04:05"))
			icon := activityIcon(a.ActivityType)
			iconStyled := lipgloss.NewStyle().
				Foreground(activityColor(a.ActivityType)).
				Render(icon)
			summary := lipgloss.NewStyle().
				Foreground(colorDimFg).
				Render(truncateMiddle(a.Summary, innerWidth-20))
			activityLines = append(activityLines,
				fmt.Sprintf("  %s  %s  %s", ts, iconStyled, summary))
		}

		activityBlock = styleSectionLabel.Render("\u2500\u2500 Activities") + "\n" +
			strings.Join(activityLines, "\n")
	}

	// ── Pending approval ────────────────────────────────────────
	pendingBlock := ""
	if len(s.PendingTools) > 0 {
		var lines []string
		for _, pt := range s.PendingTools {
			marker := safetyMarker(pt.Safety)
			toolStyle := lipgloss.NewStyle().Foreground(colorFg).Bold(true)
			detail := toolDetail(pt, innerWidth-20)
			toolLabel := pt.ToolName
			if detail != "" {
				toolLabel += ": " + detail
			}
			lines = append(lines,
				fmt.Sprintf("  %s %s", marker, toolStyle.Render(toolLabel)))
		}
		countBadge := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(colorOrange).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%d", len(s.PendingTools)))
		pendingBlock = styleSectionLabel.
			Foreground(colorOrange).
			Render("\u2500\u2500 Pending Approval") +
			" " + countBadge + "\n" +
			strings.Join(lines, "\n")
	}

	// ── Motivation (last assistant text) ────────────────────────
	motivationBlock := ""
	if s.LastText != "" {
		text := s.LastText
		maxChars := innerWidth * 3
		if len(text) > maxChars {
			text = text[:maxChars] + "\u2026"
		}
		motivationBlock = styleSectionLabel.Render("\u2500\u2500 Last Output") + "\n" +
			styleMotivation.Width(innerWidth).Render("  \u201c"+text+"\u201d")
	}

	// ── Separator line ──────────────────────────────────────────
	sep := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(innerWidth, 60)))

	// ── Assemble sections ───────────────────────────────────────
	sections := []string{header}
	if autopilotLine != "" {
		sections = append(sections, autopilotLine)
	}
	sections = append(sections, info)
	if lastActivityStr != "" {
		sections = append(sections, lastActivityStr)
	}
	if activityBlock != "" {
		sections = append(sections, sep, activityBlock)
	}
	if pendingBlock != "" {
		sections = append(sections, sep, pendingBlock)
	}
	if motivationBlock != "" {
		sections = append(sections, sep, motivationBlock)
	}

	body := strings.Join(sections, "\n")

	// Dynamic border color based on state.
	borderColor := stateColor(s.State)
	if len(s.PendingTools) > 0 {
		borderColor = colorOrange
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Background(colorPanelBg).
		Width(width - 2).
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

// activityColor returns a color for activity type icons.
func activityColor(actType string) lipgloss.Color {
	switch actType {
	case "tool_use":
		return colorAccent
	case "text":
		return colorRunning
	case "thinking":
		return colorWaiting
	case "user_message":
		return lipgloss.Color("#38bdf8") // sky blue
	case "system":
		return colorOrange
	default:
		return colorDimFg
	}
}

// toolDetail extracts a human-readable summary from a pending tool's input.
func toolDetail(pt client.PendingTool, maxLen int) string {
	if pt.ToolInput == nil {
		return ""
	}
	// Try well-known keys in order of usefulness.
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
		return styleSafe.Render("\u2713") // checkmark
	case "destructive":
		return styleDestructive.Render("\u26a0") // warning
	default:
		return styleUnknown.Render("\u2022") // bullet
	}
}
