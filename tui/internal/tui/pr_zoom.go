package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

// renderPRZoom renders the PR detail panel.
func renderPRZoom(pr client.TrackedPR, width, height int, scrollOffset int) string {
	if width < 10 || height < 4 {
		return ""
	}

	innerWidth := width - 4

	// ── Fixed header (2 lines) ──
	var headerLines []string

	// Line 1: owner/repo#number  title → base   state
	prRef := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
	refStyle := lipgloss.NewStyle().Foreground(colorAccent)
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.ANSIColor(0)).
		Background(prStateColor(pr.State)).
		Bold(true).
		Padding(0, 1)

	line1 := "  " + hyperlink(pr.URL, refStyle.Render(prRef)) + "  " +
		styleZoomHeader.Render(pr.Title) + " " +
		stateStyle.Render(prStateLabel(pr.State))

	// Autopilot badge.
	switch pr.AutopilotMode {
	case "auto":
		line1 += " " + styleAutopilotOn.Render("⚙ AUTO")
	case "yolo":
		line1 += " " + styleAutopilotWarn.Render("⚠ YOLO")
	}
	if pr.Hammer {
		line1 += " " + lipgloss.NewStyle().Foreground(colorOrange).Bold(true).Render("🔨")
	}
	if !pr.ReviewEnabled {
		line1 += " " + lipgloss.NewStyle().Foreground(colorDimFg).Render("review off")
	}
	headerLines = append(headerLines, line1)

	// Line 2: branch → base  +42 -12  3 commits  mergeable
	var infoParts []string
	infoParts = append(infoParts, pr.HeadBranch+" → "+pr.BaseBranch)
	infoParts = append(infoParts, fmt.Sprintf("+%d -%d", pr.Additions, pr.Deletions))
	infoParts = append(infoParts, fmt.Sprintf("%d commits", pr.CommitCount))
	if pr.Mergeable == "CONFLICTING" {
		infoParts = append(infoParts, lipgloss.NewStyle().Foreground(colorDestructive).Render("conflicts"))
	}
	if pr.AutopilotMode == "auto" || pr.AutopilotMode == "yolo" {
		infoParts = append(infoParts, "automerge")
	}
	if pr.AgentCostUSD > 0 {
		infoParts = append(infoParts, lipgloss.NewStyle().Foreground(colorDimFg).
			Render(fmt.Sprintf("$%.2f", pr.AgentCostUSD)))
	}
	headerLines = append(headerLines, "  "+lipgloss.NewStyle().Foreground(colorDimFg).
		Render(strings.Join(infoParts, "  ")))

	headerHeight := len(headerLines)
	bodyHeight := height - headerHeight

	// ── Scrollable body ──
	var bodyLines []string

	sep := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", min(innerWidth, 60)))

	// ── Done state: merged or closed PRs ──
	isDone := pr.State == "merged" || pr.State == "closed"
	if isDone {
		var doneMsg string
		if pr.State == "merged" {
			doneMsg = lipgloss.NewStyle().
				Foreground(colorDimFg).
				Render("  \u2714 Merged \u2014 no further action required")
		} else {
			doneMsg = lipgloss.NewStyle().
				Foreground(colorDimFg).
				Render("  \u25cf Closed \u2014 no further action required")
		}
		bodyLines = append(bodyLines, doneMsg)
		bodyLines = append(bodyLines, sep)
	}

	// ── Merge readiness summary line (skip for done PRs) ──
	if !isDone {
		var summaryParts []string

		// Approval status.
		approved := false
		changesRequested := false
		for _, r := range pr.Reviews {
			if r.State == "APPROVED" {
				approved = true
			}
			if r.State == "CHANGES_REQUESTED" {
				changesRequested = true
			}
		}
		if changesRequested {
			summaryParts = append(summaryParts,
				styleDestructive.Render("✗")+" "+
					lipgloss.NewStyle().Foreground(colorDestructive).Render("changes requested"))
		} else if approved {
			summaryParts = append(summaryParts,
				styleSafe.Render("✓")+" "+
					lipgloss.NewStyle().Foreground(colorDimFg).Render("approved"))
		} else if len(pr.Reviews) == 0 {
			summaryParts = append(summaryParts,
				lipgloss.NewStyle().Foreground(colorDimFg).Render("○ no review"))
		}

		// Checks summary.
		if len(pr.Checks) > 0 {
			passing, total := 0, len(pr.Checks)
			for _, c := range pr.Checks {
				if c.Conclusion == "SUCCESS" || c.Conclusion == "NEUTRAL" {
					passing++
				}
			}
			if passing == total {
				summaryParts = append(summaryParts,
					styleSafe.Render("✓")+" "+
						lipgloss.NewStyle().Foreground(colorDimFg).
						Render(fmt.Sprintf("checks (%d/%d)", passing, total)))
			} else {
				summaryParts = append(summaryParts,
					styleDestructive.Render("✗")+" "+
						lipgloss.NewStyle().Foreground(colorDestructive).
						Render(fmt.Sprintf("checks (%d/%d)", passing, total)))
			}
		}

		// Mergeable.
		switch pr.Mergeable {
		case "MERGEABLE":
			summaryParts = append(summaryParts,
				styleSafe.Render("✓")+" "+
					lipgloss.NewStyle().Foreground(colorDimFg).Render("mergeable"))
		case "CONFLICTING":
			summaryParts = append(summaryParts,
				styleDestructive.Render("✗")+" "+
					lipgloss.NewStyle().Foreground(colorDestructive).Render("conflicts"))
		}

		// Merge method.
		if pr.MergeMethod != "" {
			summaryParts = append(summaryParts,
				lipgloss.NewStyle().Foreground(colorAccent).Render("⎇ "+pr.MergeMethod))
		} else {
			summaryParts = append(summaryParts,
				lipgloss.NewStyle().Foreground(colorWaiting).Render("⎇ unset"))
		}

		if len(summaryParts) > 0 {
			bodyLines = append(bodyLines,
				"  "+strings.Join(summaryParts, "  "))
			bodyLines = append(bodyLines, sep)
		}
	}

	// Checks section.
	if len(pr.Checks) > 0 {
		passing, total := 0, len(pr.Checks)
		for _, c := range pr.Checks {
			if c.Conclusion == "SUCCESS" || c.Conclusion == "NEUTRAL" {
				passing++
			}
		}
		bodyLines = append(bodyLines, styleSectionLabel.Render(
			fmt.Sprintf("── Checks (%d/%d passing)", passing, total)))

		for _, c := range pr.Checks {
			icon := checkIcon(c)
			name := lipgloss.NewStyle().Foreground(colorFg).Render(c.Name)
			status := checkStatusText(c)
			dur := ""
			if c.Duration != "" {
				dur = "  " + lipgloss.NewStyle().Foreground(colorDimFg).Render(c.Duration)
			}
			detail := ""
			if c.Detail != "" {
				detail = "  " + lipgloss.NewStyle().Foreground(colorDimFg).Italic(true).
					Render(truncateMiddle(c.Detail, innerWidth-40))
			}
			bodyLines = append(bodyLines, fmt.Sprintf("  %s %s  %s%s%s", icon, name, status, dur, detail))
		}
	}

	// Agent status section.
	if pr.AgentRunning != "" {
		bodyLines = append(bodyLines, sep)
		elapsed := time.Since(pr.AgentStartedAt)
		agentLabel := pr.AgentRunning
		bodyLines = append(bodyLines, styleSectionLabel.Render("── Agent"))
		bodyLines = append(bodyLines, fmt.Sprintf("  %s %s running (%s)",
			lipgloss.NewStyle().Foreground(colorWaiting).Render("🤖"),
			lipgloss.NewStyle().Foreground(colorFg).Render(agentLabel),
			lipgloss.NewStyle().Foreground(colorDimFg).Render(formatDuration(elapsed)),
		))
	}

	// Code review findings section.
	if len(pr.ReviewFindings) > 0 {
		bodyLines = append(bodyLines, sep)
		actionable := 0
		for _, f := range pr.ReviewFindings {
			if f.Severity == "critical" || f.Severity == "important" {
				actionable++
			}
		}
		bodyLines = append(bodyLines, styleSectionLabel.Render(
			fmt.Sprintf("── Code Review (%d issues, %d actionable)", len(pr.ReviewFindings), actionable)))
		for _, f := range pr.ReviewFindings {
			var icon string
			switch f.Severity {
			case "critical":
				icon = styleDestructive.Render("✗")
			case "important":
				icon = lipgloss.NewStyle().Foreground(colorOrange).Render("⚠")
			default:
				icon = lipgloss.NewStyle().Foreground(colorDimFg).Render("○")
			}
			sev := lipgloss.NewStyle().Foreground(colorDimFg).Render("[" + f.Severity + "]")
			loc := f.File
			if f.Line > 0 {
				loc += fmt.Sprintf(":%d", f.Line)
			}
			locStyled := lipgloss.NewStyle().Foreground(colorFg).Render(loc)
			msg := lipgloss.NewStyle().Foreground(colorDimFg).Italic(true).
				Render(truncateMiddle(f.Message, innerWidth-40))
			bodyLines = append(bodyLines, fmt.Sprintf("  %s %s %s — %s", icon, sev, locStyled, msg))
		}
	} else if pr.ReviewState == "clean" {
		bodyLines = append(bodyLines, sep)
		bodyLines = append(bodyLines, styleSectionLabel.Render("── Code Review"))
		bodyLines = append(bodyLines, "  "+styleSafe.Render("✓")+" "+
			lipgloss.NewStyle().Foreground(colorDimFg).Render("Clean — no issues found"))
	}

	// Reviews section.
	if len(pr.Reviews) > 0 {
		bodyLines = append(bodyLines, sep)
		bodyLines = append(bodyLines, styleSectionLabel.Render("── Reviews"))
		for _, r := range pr.Reviews {
			icon := reviewIcon(r.State)
			author := lipgloss.NewStyle().Foreground(colorFg).Render("@" + r.Author)
			state := lipgloss.NewStyle().Foreground(colorDimFg).Render(strings.ToLower(r.State))
			body := ""
			if r.Body != "" {
				bodyTrunc := r.Body
				if len(bodyTrunc) > 50 {
					bodyTrunc = bodyTrunc[:50] + "…"
				}
				body = "  " + lipgloss.NewStyle().Foreground(colorDimFg).Italic(true).
					Render("\""+bodyTrunc+"\"")
			}
			at := ""
			if r.At != "" {
				at = "  " + lipgloss.NewStyle().Foreground(colorSubtle).Render(r.At)
			}
			bodyLines = append(bodyLines, fmt.Sprintf("  %s %s  %s%s%s", icon, author, state, body, at))
		}
	}

	// Timeline section.
	if len(pr.Timeline) > 0 {
		bodyLines = append(bodyLines, sep)
		bodyLines = append(bodyLines, styleSectionLabel.Render("── Timeline"))
		for _, ev := range pr.Timeline {
			ts := lipgloss.NewStyle().Foreground(colorSubtle).Render(ev.Time.Format("15:04"))
			icon := ev.Icon
			msg := lipgloss.NewStyle().Foreground(colorDimFg).Render(ev.Message)
			bodyLines = append(bodyLines, fmt.Sprintf("  %s  %s  %s", ts, icon, msg))
		}
	}

	// Clickable URL.
	bodyLines = append(bodyLines, sep)
	bodyLines = append(bodyLines, "  "+hyperlink(pr.URL,
		lipgloss.NewStyle().Foreground(colorAccent).Render(pr.URL)))

	// Scroll + clip.
	maxScroll := len(bodyLines) - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}

	visibleBody := bodyLines
	if scrollOffset > 0 && scrollOffset < len(visibleBody) {
		visibleBody = visibleBody[scrollOffset:]
	}
	if len(visibleBody) > bodyHeight {
		visibleBody = visibleBody[:bodyHeight]
	}

	// Scroll indicator.
	if maxScroll > 0 {
		scrollInfo := lipgloss.NewStyle().Foreground(colorDimFg)
		if scrollOffset > 0 {
			headerLines[len(headerLines)-1] += scrollInfo.Render(
				fmt.Sprintf(" ↑↓ %d%%", scrollOffset*100/maxScroll))
		} else {
			headerLines[len(headerLines)-1] += scrollInfo.Render(" ↓ scroll")
		}
	}

	// Assemble.
	all := append(headerLines, visibleBody...)
	if len(all) > height {
		all = all[:height]
	}
	for len(all) < height {
		all = append(all, "")
	}

	rendered := lipgloss.NewStyle().Width(width).Render(strings.Join(all, "\n"))

	// Hard clip rendered output to allocated height — lipgloss Width() can
	// wrap long lines and produce more lines than we budgeted for.
	renderedLines := strings.Split(rendered, "\n")
	if len(renderedLines) > height {
		renderedLines = renderedLines[:height]
	}
	return strings.Join(renderedLines, "\n")
}

func prStateColor(state string) lipgloss.TerminalColor {
	switch state {
	case "checks_failing":
		return colorDestructive
	case "checks_running":
		return colorWaiting
	case "checks_passing":
		return colorRunning
	case "approved":
		return colorRunning
	case "merged":
		return colorDimFg // dim gray — merged PRs should fade
	case "closed":
		return colorDimFg
	default:
		return colorDimFg
	}
}

func prStateLabel(state string) string {
	switch state {
	case "checks_failing":
		return "FAILING"
	case "checks_running":
		return "RUNNING"
	case "checks_passing":
		return "PASSING"
	case "approved":
		return "APPROVED"
	case "merged":
		return "MERGED"
	case "closed":
		return "CLOSED"
	default:
		return strings.ToUpper(state)
	}
}

func checkIcon(c client.PRCheck) string {
	switch c.Conclusion {
	case "SUCCESS":
		return styleSafe.Render("✓")
	case "FAILURE":
		return styleDestructive.Render("✗")
	case "NEUTRAL":
		return lipgloss.NewStyle().Foreground(colorDimFg).Render("○")
	default:
		if c.Status == "IN_PROGRESS" || c.Status == "QUEUED" {
			return lipgloss.NewStyle().Foreground(colorWaiting).Render("⏳")
		}
		return "•"
	}
}

func checkStatusText(c client.PRCheck) string {
	switch c.Conclusion {
	case "SUCCESS":
		return styleSafe.Render("passed")
	case "FAILURE":
		return styleDestructive.Render("failed")
	case "NEUTRAL":
		return lipgloss.NewStyle().Foreground(colorDimFg).Render("neutral")
	default:
		if c.Status == "IN_PROGRESS" {
			return lipgloss.NewStyle().Foreground(colorWaiting).Render("running")
		}
		if c.Status == "QUEUED" {
			return lipgloss.NewStyle().Foreground(colorDimFg).Render("queued")
		}
		return c.Status
	}
}

func reviewIcon(state string) string {
	switch state {
	case "APPROVED":
		return styleSafe.Render("✓")
	case "CHANGES_REQUESTED":
		return styleDestructive.Render("✗")
	case "COMMENTED":
		return lipgloss.NewStyle().Foreground(colorAccent).Render("💬")
	default:
		return lipgloss.NewStyle().Foreground(colorWaiting).Render("⏳")
	}
}

// hyperlink wraps text in OSC 8 escape sequence for clickable terminal links.
func hyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}
