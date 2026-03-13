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

	// Header: project_name (state) > branch
	stateStr := lipgloss.NewStyle().
		Foreground(stateColor(s.State)).
		Render(string(s.State))

	header := styleZoomHeader.Render(s.ProjectName) +
		" (" + stateStr + ")"
	if s.GitBranch != "" {
		header += " " + styleZoomBranch.Render("\u25b8 "+s.GitBranch)
	}

	// Autopilot indicator.
	autopilotLine := ""
	if s.Autopilot {
		apStyle := lipgloss.NewStyle().Foreground(colorRunning).Bold(true)
		if s.HasDestructive {
			apStyle = apStyle.Foreground(colorOrange)
		}
		autopilotLine = apStyle.Render("AUTOPILOT ON")
		if s.HasDestructive {
			autopilotLine += " " + styleDestructive.Render("[destructive pending]")
		}
	}

	// PID + CWD info.
	info := styleZoomBranch.Render(
		fmt.Sprintf("PID %d  %s", s.PID, truncateMiddle(s.CWD, innerWidth-12)))

	// Activities timeline (last 6).
	activityLines := []string{}
	start := 0
	if len(s.Activities) > 6 {
		start = len(s.Activities) - 6
	}
	for _, a := range s.Activities[start:] {
		ts := a.Timestamp.Format("15:04:05")
		icon := activityIcon(a.ActivityType)
		line := styleActivityLine.Render(
			fmt.Sprintf("  %s %s %s", ts, icon, truncateMiddle(a.Summary, innerWidth-20)))
		activityLines = append(activityLines, line)
	}

	activityBlock := ""
	if len(activityLines) > 0 {
		activityBlock = lipgloss.NewStyle().
			Foreground(colorDimFg).Bold(true).Render("Activities:") + "\n" +
			strings.Join(activityLines, "\n")
	}

	// Motivation (last assistant text).
	motivationBlock := ""
	if s.LastText != "" {
		text := s.LastText
		// Truncate to fit panel.
		maxChars := innerWidth * 3
		if len(text) > maxChars {
			text = text[:maxChars] + "..."
		}
		motivationBlock = lipgloss.NewStyle().
			Foreground(colorDimFg).Bold(true).Render("Last text:") + "\n" +
			styleMotivation.Width(innerWidth).Render("  "+text)
	}

	// Pending tools inline.
	pendingBlock := ""
	if len(s.PendingTools) > 0 {
		lines := []string{}
		for _, pt := range s.PendingTools {
			safetyIcon := safetyMarker(pt.Safety)
			lines = append(lines,
				fmt.Sprintf("  %s %s", safetyIcon, pt.ToolName))
		}
		pendingBlock = lipgloss.NewStyle().
			Foreground(colorOrange).Bold(true).Render("Pending approval:") + "\n" +
			strings.Join(lines, "\n")
	}

	// Last activity time.
	lastActivityStr := ""
	if s.LastActivity != nil {
		ago := time.Since(*s.LastActivity).Truncate(time.Second)
		lastActivityStr = styleZoomBranch.Render(fmt.Sprintf("Last activity: %s ago", ago))
	}

	// Assemble sections.
	sections := []string{header}
	if autopilotLine != "" {
		sections = append(sections, autopilotLine)
	}
	sections = append(sections, info)
	if lastActivityStr != "" {
		sections = append(sections, lastActivityStr)
	}
	if activityBlock != "" {
		sections = append(sections, "", activityBlock)
	}
	if pendingBlock != "" {
		sections = append(sections, "", pendingBlock)
	}
	if motivationBlock != "" {
		sections = append(sections, "", motivationBlock)
	}

	body := strings.Join(sections, "\n")

	return styleZoomPanel.
		Width(width - 2). // account for border
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
