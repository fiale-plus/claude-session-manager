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

// sessionsMsg carries a session update from the daemon.
type sessionsMsg []client.Session

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
	selectedIdx  int
	selectedSID  string // stable selection tracking by session ID
	queueVisible bool
	helpVisible  bool
	connected    bool
	width        int
	height       int
	flash        string // temporary status message
	flashStyle   lipgloss.Style
	glowPos      int
	glowDir      int // 1 or -1 for ping-pong
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

	sessions, ok := <-ch
	if !ok {
		subMu.Lock()
		subCh = nil
		subMu.Unlock()
		return disconnectedMsg{}
	}
	return sessionsMsg(sessions)
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

	case sessionsMsg:
		m.sessions = msg
		// Restore selection by session ID to prevent jumping.
		if m.selectedSID != "" {
			found := false
			for i, s := range m.sessions {
				if s.SessionID == m.selectedSID {
					m.selectedIdx = i
					found = true
					break
				}
			}
			if !found {
				if m.selectedIdx >= len(m.sessions) {
					m.selectedIdx = max(0, len(m.sessions)-1)
				}
				if m.selectedIdx >= 0 && m.selectedIdx < len(m.sessions) {
					m.selectedSID = m.sessions[m.selectedIdx].SessionID
				}
			}
		} else if m.selectedIdx >= len(m.sessions) {
			m.selectedIdx = max(0, len(m.sessions)-1)
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
			if m.selectedIdx < len(m.sessions) {
				m.selectedSID = m.sessions[m.selectedIdx].SessionID
			}
		}
		return m, nil

	case "right", "l":
		if m.selectedIdx < len(m.sessions)-1 {
			m.selectedIdx++
			m.scrollOffset = 0 // reset scroll on session change
			if m.selectedIdx < len(m.sessions) {
				m.selectedSID = m.sessions[m.selectedIdx].SessionID
			}
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

	case "esc":
		if m.helpVisible {
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
	strip := renderStrip(m.sessions, m.selectedIdx, w, m.glowPos)
	stripHeight := lipgloss.Height(strip)

	hints := renderHints(m.queueVisible, hasPending, w)
	hintsHeight := lipgloss.Height(hints)

	bottomHeight := stripHeight + hintsHeight

	// Status bar.
	statusLine := renderStatusBar(m.connected, m.sessions, m.flash, m.flashStyle, w)
	statusHeight := lipgloss.Height(statusLine)

	remainingHeight := m.height - bottomHeight - statusHeight

	// Main content area.
	mainContent := ""
	if m.queueVisible && hasPending {
		mainContent = renderQueue(m.sessions, w, remainingHeight)
	} else if sel := m.selected(); sel != nil {
		mainContent = renderZoom(*sel, w, remainingHeight, m.scrollOffset)
	} else {
		mainContent = renderEmptyState(w, remainingHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		statusLine,
		mainContent,
		hints,
		strip,
	)
}

// renderStatusBar renders the top status bar with branding, connection info, and flash.
func renderStatusBar(connected bool, sessions []client.Session, flash string, flashStyle lipgloss.Style, width int) string {
	logo := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		Render("\u2588\u2588 CSM")

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

	pendingStr := ""
	pending := countPending(sessions)
	if pending > 0 {
		pendingStr = lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true).
			Render(fmt.Sprintf("  \u26a1 %d pending", pending))
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

	left := logo + "  " + connStatus + "  " + sessionCount + runningStr + pendingStr

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
	subCh <-chan []client.Session
)
