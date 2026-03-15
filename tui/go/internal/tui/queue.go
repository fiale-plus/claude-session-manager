package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// renderQueue renders the pending approval queue overlay showing
// all pending tools across all sessions.
func renderQueue(sessions []client.Session, width, height int) string {
	innerWidth := width - 8
	if innerWidth < 30 {
		innerWidth = 30
	}

	// Title with count badge.
	total := countPending(sessions)
	countBadge := lipgloss.NewStyle().
		Foreground(lipgloss.ANSIColor(15)).
		Background(colorOrange).
		Bold(true).
		Padding(0, 1).
		Render(fmt.Sprintf("%d", total))

	title := styleQueueTitle.Render("\u26a1 Pending Approval Queue") + " " + countBadge

	sep := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("\u2500", min(innerWidth, 60)))

	var lines []string
	for _, s := range sessions {
		if len(s.PendingTools) == 0 {
			continue
		}

		name := s.ProjectName
		if name == "" {
			name = s.SessionID[:min(8, len(s.SessionID))]
		}

		sessionID := s.SessionID[:min(8, len(s.SessionID))]
		sessionHeader := styleZoomHeader.Render(name) +
			"  " + lipgloss.NewStyle().
			Foreground(colorSubtle).
			Render("["+sessionID+"]")

		lines = append(lines, sessionHeader)

		for _, pt := range s.PendingTools {
			marker := safetyMarker(pt.Safety)
			toolStyle := lipgloss.NewStyle().Foreground(colorFg).Bold(true)
			toolLine := fmt.Sprintf("  %s %s", marker, toolStyle.Render(pt.ToolName))

			// Show key details of tool input.
			detail := toolInputSummary(pt)
			if detail != "" {
				detailWidth := innerWidth - 14
				if detailWidth < 20 {
					detailWidth = 20
				}
				detail = truncateMiddle(detail, detailWidth)
				toolLine += "  " + lipgloss.NewStyle().
					Foreground(colorDimFg).
					Italic(true).
					Render(detail)
			}

			lines = append(lines, toolLine)
		}
		lines = append(lines, "")
	}

	if len(lines) == 0 {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(colorDimFg).
			Render("  No pending approvals"))
	}

	// Inline hint bar at bottom of queue.
	queueHints := styleHintKey.Render("y") + " approve" +
		styleHintSep.Render(" \u2502 ") +
		styleHintKey.Render("n") + " reject" +
		styleHintSep.Render(" \u2502 ") +
		styleHintKey.Render("A") + " approve all safe" +
		styleHintSep.Render(" \u2502 ") +
		styleHintKey.Render("Esc") + " close"

	body := title + "\n" + sep + "\n" +
		strings.Join(lines, "\n") + "\n" + sep + "\n" +
		styleHintsBar.Render(queueHints)

	panelHeight := height
	if panelHeight < 6 {
		panelHeight = 6
	}

	return styleQueuePanel.
		Width(width - 2).
		Height(panelHeight).
		Render(body)
}

// toolInputSummary extracts a short summary string from tool input.
func toolInputSummary(pt client.PendingTool) string {
	switch pt.ToolName {
	case "Bash":
		if cmd, ok := pt.ToolInput["command"]; ok {
			return fmt.Sprintf("%v", cmd)
		}
	case "Read":
		if fp, ok := pt.ToolInput["file_path"]; ok {
			return fmt.Sprintf("%v", fp)
		}
	case "Write":
		if fp, ok := pt.ToolInput["file_path"]; ok {
			return fmt.Sprintf("%v", fp)
		}
	case "Edit":
		if fp, ok := pt.ToolInput["file_path"]; ok {
			return fmt.Sprintf("%v", fp)
		}
	case "Grep":
		if p, ok := pt.ToolInput["pattern"]; ok {
			return fmt.Sprintf("%v", p)
		}
	case "Glob":
		if p, ok := pt.ToolInput["pattern"]; ok {
			return fmt.Sprintf("%v", p)
		}
	}
	// Fallback: show first key=value.
	for k, v := range pt.ToolInput {
		return fmt.Sprintf("%s: %v", k, v)
	}
	return ""
}
