// Package tui implements the Bubble Tea TUI for Claude Session Manager.
package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// stateMsg carries a state update (sessions + PRs) from the daemon.
type stateMsg client.StateUpdate

// connectedMsg signals that the subscription was established.
type connectedMsg struct{}

// disconnectedMsg signals that the subscription was lost.
type disconnectedMsg struct{}

// reconnectTickMsg triggers a reconnection attempt.
type reconnectTickMsg struct{}

// glowTickMsg advances the glow sweep animation.
type glowTickMsg struct{}

// actionResultMsg carries the result of an approve/reject/autopilot action.
type actionResultMsg struct {
	action string // "approve", "reject", "autopilot"
	err    error
}

// clearFlashMsg clears the status flash after a delay.
type clearFlashMsg struct{}

// Model is the top-level Bubble Tea model.
type Model struct {
	client       *client.Client
	sessions     []client.Session
	prs          []client.TrackedPR
	selectedIdx  int
	// Total items in strip = len(sessions) + len(prs).
	// Index 0..len(sessions)-1 = sessions, len(sessions)..end = PRs.
	selectedSID    string // stable selection tracking by session ID
	selectedPRKey  string // stable selection tracking by PR key (owner/repo#N)
	queueVisible bool
	helpVisible  bool
	connected    bool
	width        int
	height       int
	flash        string // temporary status message
	flashStyle   lipgloss.Style
	glowPos      int
	glowDir      int // 1 or -1 for ping-pong
	inputMode          bool              // text input active (for + add PR)
	inputBuffer        string            // text being typed
	mergePickerVisible bool              // merge strategy picker showing
	mergePickerPR      *client.TrackedPR // PR being merged
	scrollOffset int // scroll position in zoom body
}

// NewModel creates a new TUI model.
func NewModel(c *client.Client) Model {
	return Model{
		client: c,
	}
}

// glowTick returns a command that sends a glowTickMsg every 150ms.
func glowTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(_ time.Time) tea.Msg {
		return glowTickMsg{}
	})
}

// Init starts the subscription and the glow animation.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.subscribeCmd(), glowTick())
}

// subscribeCmd attempts to connect and subscribe to the daemon.
func (m Model) subscribeCmd() tea.Cmd {
	return func() tea.Msg {
		ch, err := m.client.Subscribe()
		if err != nil {
			return disconnectedMsg{}
		}

		go func() {
			_ = ch
		}()

		subMu.Lock()
		subCh = ch
		subMu.Unlock()

		return connectedMsg{}
	}
}

// waitForUpdate waits for the next session update from the subscription channel.
func waitForUpdate() tea.Msg {
	subMu.Lock()
	ch := subCh
	subMu.Unlock()

	if ch == nil {
		return disconnectedMsg{}
	}

	update, ok := <-ch
	if !ok {
		subMu.Lock()
		subCh = nil
		subMu.Unlock()
		return disconnectedMsg{}
	}
	return stateMsg(update)
}

// reconnectAfter returns a command that waits then triggers reconnect.
func reconnectAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return reconnectTickMsg{}
	})
}

// clearFlashAfter returns a command that clears the flash after a delay.
func clearFlashAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return clearFlashMsg{}
	})
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case connectedMsg:
		m.connected = true
		return m, waitForUpdate

	case disconnectedMsg:
		m.connected = false
		m.client.Close()
		return m, reconnectAfter(2 * time.Second)

	case reconnectTickMsg:
		return m, m.subscribeCmd()

	case glowTickMsg:
		// Advance glow position with ping-pong across max label length.
		maxLen := 20
		if m.glowDir == 0 {
			m.glowDir = 1
		}
		m.glowPos += m.glowDir
		if m.glowPos >= maxLen {
			m.glowPos = maxLen - 1
			m.glowDir = -1
		}
		if m.glowPos <= 0 {
			m.glowPos = 0
			m.glowDir = 1
		}
		return m, glowTick()

	case stateMsg:
		m.sessions = msg.Sessions
		m.prs = msg.PRs
		// Restore selection by ID to prevent jumping.
		totalItems := len(m.sessions) + len(m.prs)
		found := false
		if m.selectedPRKey != "" {
			// Selected item was a PR — find it by key.
			for i, p := range m.prs {
				key := fmt.Sprintf("%s/%s#%d", p.Owner, p.Repo, p.Number)
				if key == m.selectedPRKey {
					m.selectedIdx = len(m.sessions) + i
					found = true
					break
				}
			}
		} else if m.selectedSID != "" {
			// Selected item was a session — find by ID.
			for i, s := range m.sessions {
				if s.SessionID == m.selectedSID {
					m.selectedIdx = i
					found = true
					break
				}
			}
		}
		if !found && m.selectedIdx >= totalItems {
			m.selectedIdx = max(0, totalItems-1)
		}
		return m, waitForUpdate

	case actionResultMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("%s failed: %v", msg.action, msg.err)
			m.flashStyle = lipgloss.NewStyle().Foreground(colorDestructive).Bold(true)
		} else {
			m.flash = msg.action + " sent"
			m.flashStyle = lipgloss.NewStyle().Foreground(colorRunning)
		}
		return m, clearFlashAfter(2 * time.Second)

	case clearFlashMsg:
		m.flash = ""
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Text input mode (for + add PR).
	if m.inputMode {
		switch msg.String() {
		case "enter":
			url := strings.TrimSpace(m.inputBuffer)
			m.inputMode = false
			m.inputBuffer = ""
			if url != "" {
				return m, func() tea.Msg {
					err := m.client.AddPR(url)
					if err != nil {
						return actionResultMsg{action: "add PR", err: err}
					}
					return actionResultMsg{action: "added PR"}
				}
			}
			return m, nil
		case "esc":
			m.inputMode = false
			m.inputBuffer = ""
			return m, nil
		case "backspace":
			if len(m.inputBuffer) > 0 {
				m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.inputBuffer += msg.String()
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "q":
		if m.queueVisible || m.helpVisible {
			return m, nil
		}
		m.client.Close()
		return m, tea.Quit

	case "ctrl+c":
		m.client.Close()
		return m, tea.Quit

	case "left":
		if m.selectedIdx > 0 {
			m.selectedIdx--
			m.scrollOffset = 0
			m.trackSelection()
		}
		return m, nil

	case "right", "l":
		if m.selectedIdx < m.totalItems()-1 {
			m.selectedIdx++
			m.scrollOffset = 0
			m.trackSelection()
		}
		return m, nil

	case "up", "k":
		if m.scrollOffset > 0 {
			m.scrollOffset--
		}
		return m, nil

	case "down", "j":
		m.scrollOffset++
		return m, nil

	case "home":
		m.selectedIdx = 0
		m.scrollOffset = 0
		if len(m.sessions) > 0 {
			m.trackSelection()
		}
		return m, nil

	case "end":
		if len(m.sessions) > 0 {
			m.selectedIdx = len(m.sessions) - 1
			m.scrollOffset = 0
			m.trackSelection()
		}
		return m, nil

	case "pgup":
		m.scrollOffset -= 5
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return m, nil

	case "pgdown":
		m.scrollOffset += 5
		return m, nil

	case "h":
		m.helpVisible = !m.helpVisible
		return m, nil

	case "a":
		if sel := m.selected(); sel != nil {
			sid := sel.SessionID
			return m, func() tea.Msg {
				err := m.client.ToggleAutopilot(sid)
				return actionResultMsg{action: "autopilot toggle", err: err}
			}
		}

	case "enter", "return":
		if sel := m.selected(); sel != nil && sel.GhosttyTabIndex > 0 {
			tabIdx := sel.GhosttyTabIndex
			return m, func() tea.Msg {
				// Switch by tab index — stable unlike names with animated spinners.
				script := fmt.Sprintf(`tell application "System Events" to tell process "Ghostty"
    set tabGroup to tab group 1 of window 1
    set allButtons to every radio button of tabGroup
    if (count of allButtons) >= %d then
        click item %d of allButtons
    end if
end tell`, tabIdx, tabIdx)
				err := exec.Command("osascript", "-e", script).Run()
				if err != nil {
					return actionResultMsg{action: "focus", err: err}
				}
				return actionResultMsg{action: "focus"}
			}
		}

	case "y":
		if sel := m.selected(); sel != nil && len(sel.PendingTools) > 0 {
			sid := sel.SessionID
			return m, func() tea.Msg {
				err := m.client.Approve(sid)
				return actionResultMsg{action: "approve", err: err}
			}
		}

	case "n":
		if sel := m.selected(); sel != nil && len(sel.PendingTools) > 0 {
			sid := sel.SessionID
			return m, func() tea.Msg {
				err := m.client.Reject(sid)
				return actionResultMsg{action: "reject", err: err}
			}
		}

	case "shift+q", "Q":
		m.queueVisible = !m.queueVisible
		return m, nil

	case "shift+a", "A":
		return m, func() tea.Msg {
			err := m.client.ApproveAll()
			return actionResultMsg{action: "approve all", err: err}
		}

	case "+", "=":
		// Add PR — enter input mode.
		m.inputMode = true
		m.inputBuffer = ""
		return m, nil

	case "-":
		// Remove selected PR from tracking.
		if pr := m.selectedPR(); pr != nil {
			key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
			return m, func() tea.Msg {
				err := m.client.RemovePR(key)
				return actionResultMsg{action: "removed PR", err: err}
			}
		}

	case "o":
		// Open PR in browser.
		if pr := m.selectedPR(); pr != nil {
			url := pr.URL
			return m, func() tea.Msg {
				err := exec.Command("open", url).Run()
				return actionResultMsg{action: "opened PR", err: err}
			}
		}

	case "m":
		// Merge selected PR — show merge strategy picker.
		if pr := m.selectedPR(); pr != nil {
			m.mergePickerPR = pr
			m.mergePickerVisible = true
			return m, nil
		}

	case "1":
		// Merge picker: squash automerge.
		if m.mergePickerVisible && m.mergePickerPR != nil {
			pr := m.mergePickerPR
			m.mergePickerVisible = false
			m.mergePickerPR = nil
			owner, repo, number := pr.Owner, pr.Repo, pr.Number
			return m, func() tea.Msg {
				err := exec.Command("gh", "pr", "merge",
					fmt.Sprintf("%d", number),
					"--repo", fmt.Sprintf("%s/%s", owner, repo),
					"--squash", "--auto").Run()
				if err != nil {
					return actionResultMsg{action: "squash merge", err: err}
				}
				return actionResultMsg{action: "squash automerge enabled"}
			}
		}

	case "2":
		// Merge picker: rebase automerge.
		if m.mergePickerVisible && m.mergePickerPR != nil {
			pr := m.mergePickerPR
			m.mergePickerVisible = false
			m.mergePickerPR = nil
			owner, repo, number := pr.Owner, pr.Repo, pr.Number
			return m, func() tea.Msg {
				err := exec.Command("gh", "pr", "merge",
					fmt.Sprintf("%d", number),
					"--repo", fmt.Sprintf("%s/%s", owner, repo),
					"--rebase", "--auto").Run()
				if err != nil {
					return actionResultMsg{action: "rebase merge", err: err}
				}
				return actionResultMsg{action: "rebase automerge enabled"}
			}
		}

	case "3":
		// Merge picker: Aviator merge queue.
		if m.mergePickerVisible && m.mergePickerPR != nil {
			pr := m.mergePickerPR
			m.mergePickerVisible = false
			m.mergePickerPR = nil
			owner, repo, number := pr.Owner, pr.Repo, pr.Number
			return m, func() tea.Msg {
				err := exec.Command("gh", "pr", "comment",
					fmt.Sprintf("%d", number),
					"--repo", fmt.Sprintf("%s/%s", owner, repo),
					"--body", "/aviator merge").Run()
				if err != nil {
					return actionResultMsg{action: "aviator merge", err: err}
				}
				return actionResultMsg{action: "aviator merge queued"}
			}
		}

	case "4":
		// Merge picker: merge commit automerge.
		if m.mergePickerVisible && m.mergePickerPR != nil {
			pr := m.mergePickerPR
			m.mergePickerVisible = false
			m.mergePickerPR = nil
			owner, repo, number := pr.Owner, pr.Repo, pr.Number
			return m, func() tea.Msg {
				err := exec.Command("gh", "pr", "merge",
					fmt.Sprintf("%d", number),
					"--repo", fmt.Sprintf("%s/%s", owner, repo),
					"--merge", "--auto").Run()
				if err != nil {
					return actionResultMsg{action: "merge commit", err: err}
				}
				return actionResultMsg{action: "merge commit automerge enabled"}
			}
		}

	case "esc":
		if m.mergePickerVisible {
			m.mergePickerVisible = false
			m.mergePickerPR = nil
		} else if m.inputMode {
			m.inputMode = false
			m.inputBuffer = ""
		} else if m.helpVisible {
			m.helpVisible = false
		} else if m.queueVisible {
			m.queueVisible = false
		}
		return m, nil
	}

	return m, nil
}

// approveAllSafe sends approve for every session that has only safe pending tools.
func (m Model) approveAllSafe() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.sessions {
		for _, pt := range s.PendingTools {
			if pt.Safety != "destructive" {
				sid := s.SessionID
				cmds = append(cmds, func() tea.Msg {
					err := m.client.Approve(sid)
					return actionResultMsg{action: "approve", err: err}
				})
				break
			}
		}
	}
	return tea.Batch(cmds...)
}

// selected returns the currently selected session, or nil.
func (m Model) selected() *client.Session {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.sessions) {
		s := m.sessions[m.selectedIdx]
		return &s
	}
	return nil
}

// selectedPR returns the currently selected PR, or nil.
func (m Model) selectedPR() *client.TrackedPR {
	prIdx := m.selectedIdx - len(m.sessions)
	if prIdx >= 0 && prIdx < len(m.prs) {
		pr := m.prs[prIdx]
		return &pr
	}
	return nil
}

// isSessionSelected returns true if a session (not PR) is selected.
func (m Model) isSessionSelected() bool {
	return m.selectedIdx < len(m.sessions)
}

// totalItems returns the total number of items in the strip.
func (m Model) totalItems() int {
	return len(m.sessions) + len(m.prs)
}

// trackSelection updates selectedSID/selectedPRKey based on current selectedIdx.
func (m *Model) trackSelection() {
	if m.selectedIdx < len(m.sessions) {
		if m.selectedIdx >= 0 && m.selectedIdx < len(m.sessions) {
			m.selectedSID = m.sessions[m.selectedIdx].SessionID
			m.selectedPRKey = ""
		}
	} else {
		prIdx := m.selectedIdx - len(m.sessions)
		if prIdx >= 0 && prIdx < len(m.prs) {
			p := m.prs[prIdx]
			m.selectedPRKey = fmt.Sprintf("%s/%s#%d", p.Owner, p.Repo, p.Number)
			m.selectedSID = ""
		}
	}
}

// View renders the entire TUI.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	w := m.width
	hasPending := countPending(m.sessions) > 0

	// If help is visible, render help overlay instead.
	if m.helpVisible {
		return renderHelp(w, m.height)
	}

	// Render bottom sections first to calculate remaining height.
	strip := renderUnifiedStrip(m.sessions, m.prs, m.selectedIdx, w, m.glowPos)
	stripHeight := lipgloss.Height(strip)

	isSession := m.isSessionSelected()
	hints := renderHints(m.queueVisible, hasPending, w)
	hintsHeight := lipgloss.Height(hints)

	bottomHeight := stripHeight + hintsHeight

	// Merge strategy picker overlay.
	if m.mergePickerVisible && m.mergePickerPR != nil {
		pr := m.mergePickerPR
		picker := lipgloss.NewStyle().Padding(1, 2).Render(
			styleZoomHeader.Render(fmt.Sprintf("  Merge #%d %s\n", pr.Number, pr.Title)) + "\n" +
				lipgloss.NewStyle().Foreground(colorFg).Render(
					"  [1] Squash automerge\n"+
						"  [2] Rebase automerge\n"+
						"  [3] Aviator merge queue\n"+
						"  [4] Merge commit automerge\n"+
						"  [Esc] Cancel"))

		return lipgloss.JoinVertical(lipgloss.Left,
			picker,
			lipgloss.NewStyle().Width(w).Height(m.height-lipgloss.Height(picker)-stripHeight).Render(""),
			strip,
		)
	}

	// Input bar (for + add PR).
	if m.inputMode {
		inputStyle := lipgloss.NewStyle().Foreground(colorFg).Bold(true)
		cursorStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		inputBar := inputStyle.Render("  Add PR: ") + m.inputBuffer + cursorStyle.Render("█")
		inputBar += lipgloss.NewStyle().Foreground(colorDimFg).Render("  (paste URL or owner/repo#N, Enter to add, Esc to cancel)")

		return lipgloss.JoinVertical(lipgloss.Left,
			inputBar,
			lipgloss.NewStyle().Width(w).Height(m.height-2).Render(""),
			strip,
		)
	}

	// Status bar.
	failingPRs := 0
	for _, p := range m.prs {
		if p.State == "checks_failing" {
			failingPRs++
		}
	}
	statusLine := renderStatusBar(m.connected, m.sessions, m.prs, failingPRs, m.flash, m.flashStyle, w)
	statusHeight := lipgloss.Height(statusLine)

	remainingHeight := m.height - bottomHeight - statusHeight

	// Main content area.
	mainContent := ""
	if m.queueVisible && hasPending {
		mainContent = renderQueue(m.sessions, w, remainingHeight)
	} else if isSession {
		if sel := m.selected(); sel != nil {
			mainContent = renderZoom(*sel, w, remainingHeight, m.scrollOffset)
		} else {
			mainContent = renderEmptyState(w, remainingHeight)
		}
	} else if selPR := m.selectedPR(); selPR != nil {
		mainContent = renderPRZoom(*selPR, w, remainingHeight, m.scrollOffset)
	} else {
		mainContent = renderEmptyState(w, remainingHeight)
	}

	output := lipgloss.JoinVertical(lipgloss.Left,
		statusLine,
		mainContent,
		hints,
		strip,
	)

	// Hard clip to terminal height to prevent overflow pushing status bar off screen.
	lines := strings.Split(output, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return strings.Join(lines, "\n")
}

// renderStatusBar renders the top status bar with branding, connection info, and flash.
func renderStatusBar(connected bool, sessions []client.Session, prs []client.TrackedPR, failingPRs int, flash string, flashStyle lipgloss.Style, width int) string {
	logo := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		Render("\u2588\u2588 CCC")

	var connStatus string
	if !connected {
		connStatus = lipgloss.NewStyle().
			Foreground(colorDestructive).
			Render("\u25cf disconnected")
	} else {
		connStatus = lipgloss.NewStyle().
			Foreground(colorRunning).
			Render("\u25cf connected")
	}

	sessionCount := lipgloss.NewStyle().
		Foreground(colorDimFg).
		Render(pluralize(len(sessions), "session", "sessions"))

	prCount := ""
	if len(prs) > 0 {
		prCount = "  " + lipgloss.NewStyle().
			Foreground(colorDimFg).
			Render(pluralize(len(prs), "PR", "PRs"))
	}

	pendingStr := ""
	pending := countPending(sessions)
	if pending > 0 {
		pendingStr = lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true).
			Render(fmt.Sprintf("  \u26a1 %d pending", pending))
	}

	failingStr := ""
	if failingPRs > 0 {
		failingStr = lipgloss.NewStyle().
			Foreground(colorDestructive).
			Bold(true).
			Render(fmt.Sprintf("  \u2717 %d failing", failingPRs))
	}

	runningStr := ""
	running := 0
	for _, s := range sessions {
		if s.State == "running" {
			running++
		}
	}
	if running > 0 {
		runningStr = lipgloss.NewStyle().
			Foreground(colorRunning).
			Render(fmt.Sprintf("  \u25b6 %d running", running))
	}

	left := logo + "  " + connStatus + "  " + sessionCount + prCount + runningStr + pendingStr + failingStr

	// Flash message (action feedback).
	if flash != "" {
		left += "  " + flashStyle.Render(flash)
	}

	sep := lipgloss.NewStyle().
		Foreground(colorBorder).
		Render(strings.Repeat("\u2500", max(0, width)))

	return left + "\n" + sep
}

// renderEmptyState renders a centered empty state when no sessions exist.
func renderEmptyState(width, height int) string {
	art := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render("\u2588\u2588\u2588\u2588")

	title := lipgloss.NewStyle().
		Foreground(colorFg).
		Bold(true).
		Render("Claude Session Manager")

	subtitle := lipgloss.NewStyle().
		Foreground(colorDimFg).
		Italic(true).
		Render("Waiting for Claude Code sessions...")

	hint := lipgloss.NewStyle().
		Foreground(colorSubtle).
		Render("Start a Claude Code session to see it here")

	block := lipgloss.JoinVertical(lipgloss.Center,
		art,
		"",
		title,
		subtitle,
		"",
		hint,
	)

	return lipgloss.NewStyle().
		Height(height).
		Width(width).
		Align(lipgloss.Center, lipgloss.Center).
		Render(block)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return itoa(n) + " " + plural
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

var (
	subMu sync.Mutex
	subCh <-chan client.StateUpdate
)
