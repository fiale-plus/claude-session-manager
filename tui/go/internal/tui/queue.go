package tui

import (
	"fmt"
	"strings"

	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// renderQueue renders the pending approval queue overlay showing
// all pending tools across all sessions.
func renderQueue(sessions []client.Session, width, height int) string {
	title := styleQueueTitle.Render("Pending Approval Queue")

	var lines []string
	for _, s := range sessions {
		if len(s.PendingTools) == 0 {
			continue
		}

		name := s.ProjectName
		if name == "" {
			name = s.SessionID[:min(8, len(s.SessionID))]
		}

		sessionHeader := styleZoomHeader.Render(name) +
			" " + styleZoomBranch.Render("["+s.SessionID[:min(8, len(s.SessionID))]+"]")

		lines = append(lines, sessionHeader)

		for _, pt := range s.PendingTools {
			marker := safetyMarker(pt.Safety)
			toolLine := fmt.Sprintf("  %s %s", marker, pt.ToolName)

			// Show key details of tool input.
			detail := toolInputSummary(pt)
			if detail != "" {
				innerWidth := width - 14
				if innerWidth < 20 {
					innerWidth = 20
				}
				detail = truncateMiddle(detail, innerWidth)
				toolLine += styleZoomBranch.Render("  " + detail)
			}

			lines = append(lines, toolLine)
		}
		lines = append(lines, "")
	}

	if len(lines) == 0 {
		lines = append(lines, styleZoomBranch.Render("  No pending approvals"))
	}

	body := title + "\n\n" + strings.Join(lines, "\n") + "\n\n" +
		styleHintsBar.Render("y approve  n reject  A approve all safe  Esc close")

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
