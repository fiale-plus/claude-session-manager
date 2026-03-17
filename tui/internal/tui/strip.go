package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// renderStrip renders the horizontal session pill strip at the bottom (sessions only).
func renderStrip(sessions []client.Session, selectedIdx int, width int, glowPos int) string {
	return renderUnifiedStrip(sessions, nil, selectedIdx, width, glowPos)
}

// disambiguateNames detects duplicate pill names across sessions and appends
// a short disambiguator (PID or session ID prefix) when names collide.
func disambiguateNames(sessions []client.Session) map[string]string {
	result := make(map[string]string, len(sessions))

	// Count how many sessions share each name.
	nameCounts := make(map[string]int)
	for _, s := range sessions {
		name := pillName(s)
		nameCounts[name]++
	}

	// For duplicates, append disambiguator.
	nameSeq := make(map[string]int)
	for _, s := range sessions {
		name := pillName(s)
		if nameCounts[name] > 1 {
			nameSeq[name]++
			if s.PID > 0 {
				result[s.SessionID] = fmt.Sprintf("%s:%d", name, s.PID)
			} else if len(s.SessionID) >= 4 {
				result[s.SessionID] = fmt.Sprintf("%s~%s", name, s.SessionID[:4])
			} else {
				result[s.SessionID] = fmt.Sprintf("%s#%d", name, nameSeq[name])
			}
		} else {
			result[s.SessionID] = name
		}
	}
	return result
}

// statePriority returns lower numbers for higher-priority (more urgent) states.
func statePriority(s client.Session) int {
	// Sessions with pending tools need attention first.
	if len(s.PendingTools) > 0 {
		return 0
	}
	switch s.State {
	case "running":
		return 1
	case "waiting":
		return 2
	case "idle":
		return 3
	case "dead":
		return 4
	default:
		return 5
	}
}

// renderUnifiedStrip renders sessions + PRs in one strip with a separator.
// It caps visible pills to fit within the given width, showing a "+N"
// overflow indicator when pills are hidden.
func renderUnifiedStrip(sessions []client.Session, prs []client.TrackedPR, selectedIdx int, width int, glowPos int) string {
	if len(sessions) == 0 && len(prs) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(colorDimFg).Italic(true)
		return styleStripBar.Width(width).Render(
			emptyStyle.Render("  No active sessions or PRs"))
	}

	// Sort sessions by attention priority: pending > running > waiting > idle > dead.
	// Track the selected session ID so we can remap selectedIdx after sorting.
	var selectedSessionID string
	if selectedIdx >= 0 && selectedIdx < len(sessions) {
		selectedSessionID = sessions[selectedIdx].SessionID
	}

	// Work on a copy to avoid mutating the caller's slice.
	sortedSessions := make([]client.Session, len(sessions))
	copy(sortedSessions, sessions)
	sort.SliceStable(sortedSessions, func(i, j int) bool {
		return statePriority(sortedSessions[i]) < statePriority(sortedSessions[j])
	})

	// Remap selectedIdx to new position in sorted slice.
	if selectedSessionID != "" {
		for i, s := range sortedSessions {
			if s.SessionID == selectedSessionID {
				selectedIdx = i
				break
			}
		}
	}
	sessions = sortedSessions

	// Pre-compute disambiguated names for sessions.
	nameMap := disambiguateNames(sessions)

	// Available width inside the strip bar (account for Padding(0,1) = 2 chars
	// and the top border which doesn't affect horizontal space).
	budget := width - 2
	if budget < 10 {
		budget = 10
	}

	type pillEntry struct {
		rendered string
		width    int
		isSelected bool
	}

	// Build all pill entries.
	var allPills []pillEntry
	for i, s := range sessions {
		p := renderPillWithName(s, nameMap[s.SessionID], i == selectedIdx, glowPos)
		allPills = append(allPills, pillEntry{
			rendered:   p,
			width:      lipgloss.Width(p),
			isSelected: i == selectedIdx,
		})
	}

	// Filter terminal PRs when active ones exist: hide merged/closed PRs from
	// the visible strip and show a compact count indicator instead.
	visiblePRs := prs
	doneCount := 0
	hasActivePR := false
	for _, p := range prs {
		if p.State != "merged" && p.State != "closed" {
			hasActivePR = true
			break
		}
	}
	if hasActivePR {
		var filtered []client.TrackedPR
		for _, p := range prs {
			if p.State == "merged" || p.State == "closed" {
				doneCount++
				// If this PR is selected, include it anyway so selection stays valid.
				prIdx := len(sessions) + len(filtered)
				_ = prIdx
			} else {
				filtered = append(filtered, p)
			}
		}
		// Remap selectedIdx if we filtered out PRs before the selected one.
		if selectedIdx >= len(sessions) {
			origPRIdx := selectedIdx - len(sessions)
			if origPRIdx < len(prs) {
				selectedPR := prs[origPRIdx]
				if selectedPR.State == "merged" || selectedPR.State == "closed" {
					// Selected PR was filtered; keep it visible.
					filtered = append(filtered, selectedPR)
					selectedIdx = len(sessions) + len(filtered) - 1
				} else {
					// Remap to new position in filtered slice.
					for newI, p := range filtered {
						if p.Number == selectedPR.Number && p.Owner == selectedPR.Owner {
							selectedIdx = len(sessions) + newI
							break
						}
					}
				}
			}
		}
		visiblePRs = filtered
	}

	// Separator between sessions and PRs.
	hasSep := len(sessions) > 0 && len(visiblePRs) > 0
	sepStr := ""
	sepWidth := 0
	if hasSep {
		sepStr = lipgloss.NewStyle().Foreground(colorBorder).Render("│")
		sepWidth = lipgloss.Width(sepStr) + 2 // " │ " with surrounding spaces
	}

	for i, p := range visiblePRs {
		prIdx := len(sessions) + i
		pill := renderPRPill(p, prIdx == selectedIdx)
		allPills = append(allPills, pillEntry{
			rendered:   pill,
			width:      lipgloss.Width(pill),
			isSelected: prIdx == selectedIdx,
		})
	}

	// Append a compact "done" indicator if any PRs were filtered.
	if doneCount > 0 {
		doneStr := lipgloss.NewStyle().Foreground(colorDimFg).Render(fmt.Sprintf("(+%d done)", doneCount))
		// Add separator if no visible PRs were rendered (only done PRs).
		if !hasSep && len(sessions) > 0 {
			sepStr = lipgloss.NewStyle().Foreground(colorBorder).Render("│")
			sepWidth = lipgloss.Width(sepStr) + 2
			hasSep = true
		}
		allPills = append(allPills, pillEntry{
			rendered:   doneStr,
			width:      lipgloss.Width(doneStr),
			isSelected: false,
		})
	}

	// Fit pills within budget, always including the selected pill.
	// Strategy: include pills left-to-right until budget exhausted.
	// If selected pill would be excluded, shift the visible window.
	overflowStyle := lipgloss.NewStyle().Foreground(colorDimFg).Bold(true)

	spaceWidth := 1 // " " between pills
	overflowBase := lipgloss.Width(overflowStyle.Render("+99"))

	// Calculate which pills to show.
	totalPills := len(allPills)
	if totalPills == 0 {
		return styleStripBar.Width(width).Render("")
	}

	// Find which range of pills fits, centered on the selected pill.
	selectedPill := selectedIdx
	// Account for the separator being in a different position:
	// allPills has sessions then PRs (no separator entry).
	// We insert separator visually, not as a pill.
	if selectedPill < 0 {
		selectedPill = 0
	}
	if selectedPill >= totalPills {
		selectedPill = totalPills - 1
	}

	// Try to fit all pills first.
	totalWidth := 0
	for i, p := range allPills {
		totalWidth += p.width
		if i > 0 {
			totalWidth += spaceWidth
		}
	}
	if hasSep {
		totalWidth += sepWidth
	}

	if totalWidth <= budget {
		// Everything fits — render all.
		var pills []string
		for i, p := range allPills {
			if hasSep && i == len(sessions) {
				pills = append(pills, sepStr)
			}
			pills = append(pills, p.rendered)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Center, interleave(pills, " ")...)
		return styleStripBar.Width(width).Render(row)
	}

	// Not everything fits — build a visible window around the selected pill.
	// Start with the selected pill and expand outward.
	visStart := selectedPill
	visEnd := selectedPill // inclusive

	usedWidth := allPills[selectedPill].width

	// Expand alternately left and right.
	for {
		expanded := false
		// Try right.
		if visEnd+1 < totalPills {
			nextW := allPills[visEnd+1].width + spaceWidth
			// Account for separator if crossing the boundary.
			if hasSep && visEnd+1 == len(sessions) {
				nextW += sepWidth
			}
			// Reserve space for left overflow indicator.
			leftOverflow := 0
			if visStart > 0 {
				leftOverflow = overflowBase + spaceWidth
			}
			rightOverflow := 0
			if visEnd+2 < totalPills {
				rightOverflow = overflowBase + spaceWidth
			}
			if usedWidth+nextW+leftOverflow+rightOverflow <= budget {
				visEnd++
				usedWidth += nextW
				expanded = true
			}
		}
		// Try left.
		if visStart-1 >= 0 {
			nextW := allPills[visStart-1].width + spaceWidth
			if hasSep && visStart == len(sessions) {
				nextW += sepWidth
			}
			leftOverflow := 0
			if visStart-2 >= 0 {
				leftOverflow = overflowBase + spaceWidth
			}
			rightOverflow := 0
			if visEnd+1 < totalPills {
				rightOverflow = overflowBase + spaceWidth
			}
			if usedWidth+nextW+leftOverflow+rightOverflow <= budget {
				visStart--
				usedWidth += nextW
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	// Build visible pills with overflow indicators.
	var pills []string
	if visStart > 0 {
		pills = append(pills, overflowStyle.Render(fmt.Sprintf("+%d", visStart)))
	}
	for i := visStart; i <= visEnd; i++ {
		if hasSep && i == len(sessions) && visStart <= len(sessions)-1 {
			pills = append(pills, sepStr)
		}
		pills = append(pills, allPills[i].rendered)
	}
	if visEnd < totalPills-1 {
		pills = append(pills, overflowStyle.Render(fmt.Sprintf("+%d", totalPills-1-visEnd)))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Center, interleave(pills, " ")...)
	return styleStripBar.Width(width).Render(row)
}

// renderPRPill renders a single PR pill in the strip.
func renderPRPill(p client.TrackedPR, selected bool) string {
	icon := prPillIcon(p.State)

	// For merged PRs, just show "#N" since the title no longer matters.
	var label string
	if p.State == "merged" || p.State == "closed" {
		label = fmt.Sprintf("%s #%d", icon, p.Number)
	} else {
		label = fmt.Sprintf("%s #%d %s", icon, p.Number, truncateWordBoundary(p.Title, 15))
	}

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
		return "\u2717"
	case "checks_running":
		return "\u23f3"
	case "checks_passing":
		return "\u2713"
	case "approved":
		return "\u2713"
	case "merged":
		return "\u2713"
	default:
		return "\u2022"
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

// truncateWordBoundary truncates a string at a word boundary if it exceeds maxLen.
// Unlike truncateMiddle, it avoids cutting mid-word.
func truncateWordBoundary(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	// Find the last space before maxLen-1 (leaving room for ellipsis).
	cutoff := maxLen - 1
	lastSpace := -1
	for i := cutoff; i >= 0; i-- {
		if runes[i] == ' ' || runes[i] == '-' || runes[i] == '/' {
			lastSpace = i
			break
		}
	}
	if lastSpace > maxLen/3 {
		return strings.TrimRight(string(runes[:lastSpace]), " ") + "\u2026"
	}
	// No good break point — just truncate.
	return string(runes[:maxLen-1]) + "\u2026"
}

// stripXMLTags removes XML/HTML tags from a string (e.g. <task-notification>).
var xmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripXMLTags(s string) string {
	return xmlTagRe.ReplaceAllString(s, "")
}

// containsCCInternalMarkup returns true if the string looks like CC internal
// messaging (task notifications, etc.) that should be filtered out.
func containsCCInternalMarkup(s string) bool {
	return strings.Contains(s, "<task-") || strings.Contains(s, "<tool_") ||
		strings.Contains(s, "</task-") || strings.Contains(s, "<notification")
}

// stripMarkdown removes common markdown formatting from a string:
// **, ##, |, table dividers (---|---), etc.
func stripMarkdown(s string) string {
	// Remove bold/italic markers.
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	// Remove heading markers.
	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip pure table divider lines (---|---|---).
		if len(trimmed) > 0 && isTableDivider(trimmed) {
			continue
		}
		// Strip leading ## markers.
		for strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimPrefix(trimmed, "#")
		}
		trimmed = strings.TrimSpace(trimmed)
		// Strip leading/trailing pipe characters (table rows).
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			trimmed = strings.Trim(trimmed, "| ")
			// Replace inner pipes with commas for readability.
			trimmed = strings.ReplaceAll(trimmed, " | ", ", ")
			trimmed = strings.ReplaceAll(trimmed, "|", ", ")
		}
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, " ")
}

// isTableDivider returns true if a line is a markdown table divider like ---|---|---.
func isTableDivider(s string) bool {
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "|", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.TrimSpace(s)
	return s == ""
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
