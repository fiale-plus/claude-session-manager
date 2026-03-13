// Package tui implements the Bubble Tea TUI for Claude Session Manager.
package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
)

// sessionsMsg carries a session update from the daemon.
type sessionsMsg []client.Session

// connectedMsg signals that the subscription was established.
type connectedMsg struct{}

// disconnectedMsg signals that the subscription was lost.
type disconnectedMsg struct{}

// reconnectTickMsg triggers a reconnection attempt.
type reconnectTickMsg struct{}

// Model is the top-level Bubble Tea model.
type Model struct {
	client       *client.Client
	sessions     []client.Session
	selectedIdx  int
	queueVisible bool
	connected    bool
	width        int
	height       int
}

// NewModel creates a new TUI model.
func NewModel(c *client.Client) Model {
	return Model{
		client: c,
	}
}

// Init starts the subscription.
func (m Model) Init() tea.Cmd {
	return m.subscribeCmd()
}

// subscribeCmd attempts to connect and subscribe to the daemon.
func (m Model) subscribeCmd() tea.Cmd {
	return func() tea.Msg {
		ch, err := m.client.Subscribe()
		if err != nil {
			return disconnectedMsg{}
		}

		// We need to return connectedMsg first, then start reading.
		// We'll spawn a goroutine to feed updates and return connected.
		go func() {
			// This goroutine is just a pump — it does nothing once ch closes
			// because the program will receive disconnectedMsg from waitForUpdate.
			_ = ch // kept alive by the waitForUpdate chain
		}()

		// Store the channel in a package-level var so waitForUpdate can read it.
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

	case sessionsMsg:
		m.sessions = msg
		// Clamp selected index.
		if m.selectedIdx >= len(m.sessions) {
			m.selectedIdx = max(0, len(m.sessions)-1)
		}
		return m, waitForUpdate
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		if m.queueVisible {
			// Don't quit if queue is open — 'q' is too close to 'Q'.
			return m, nil
		}
		m.client.Close()
		return m, tea.Quit

	case "ctrl+c":
		m.client.Close()
		return m, tea.Quit

	case "left", "h":
		if m.selectedIdx > 0 {
			m.selectedIdx--
		}
		return m, nil

	case "right", "l":
		if m.selectedIdx < len(m.sessions)-1 {
			m.selectedIdx++
		}
		return m, nil

	case "a":
		if sel := m.selected(); sel != nil {
			return m, func() tea.Msg {
				_ = m.client.ToggleAutopilot(sel.SessionID)
				return nil
			}
		}

	case "enter", "y":
		if sel := m.selected(); sel != nil && len(sel.PendingTools) > 0 {
			return m, func() tea.Msg {
				_ = m.client.Approve(sel.SessionID)
				return nil
			}
		}

	case "n":
		if sel := m.selected(); sel != nil && len(sel.PendingTools) > 0 {
			return m, func() tea.Msg {
				_ = m.client.Reject(sel.SessionID)
				return nil
			}
		}

	case "shift+q", "Q":
		m.queueVisible = !m.queueVisible
		return m, nil

	case "shift+a", "A":
		// Approve all safe pending tools.
		return m, m.approveAllSafe()

	case "esc":
		if m.queueVisible {
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
					_ = m.client.Approve(sid)
					return nil
				})
				break // one approve per session
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

	// Render bottom sections first to calculate remaining height.
	strip := renderStrip(m.sessions, m.selectedIdx, w)
	stripHeight := lipgloss.Height(strip)

	hints := renderHints(m.queueVisible, hasPending, w)
	hintsHeight := lipgloss.Height(hints)

	bottomHeight := stripHeight + hintsHeight

	// Status indicator.
	statusLine := ""
	if !m.connected {
		statusLine = styleStatusDisconnected.Render(" disconnected - reconnecting...")
	} else {
		statusLine = styleStatusConnected.Render(" connected") +
			styleZoomBranch.Render(
				"  " + pluralize(len(m.sessions), "session", "sessions"))
	}
	statusHeight := 1

	remainingHeight := m.height - bottomHeight - statusHeight

	// Main content area.
	mainContent := ""
	if m.queueVisible && hasPending {
		// Queue overlay takes over the main area.
		queueHeight := remainingHeight - 2
		if queueHeight < 6 {
			queueHeight = 6
		}
		mainContent = renderQueue(m.sessions, w, queueHeight)
	} else if sel := m.selected(); sel != nil {
		// Zoom panel for selected session.
		zoomHeight := remainingHeight - 2
		if zoomHeight < 4 {
			zoomHeight = 4
		}
		mainContent = renderZoom(*sel, w, zoomHeight)
	} else {
		// No sessions.
		noSessions := lipgloss.NewStyle().
			Foreground(colorDimFg).
			Width(w).
			Align(lipgloss.Center).
			Render("Waiting for Claude Code sessions...")
		mainContent = lipgloss.NewStyle().
			Height(remainingHeight).
			Width(w).
			Align(lipgloss.Center, lipgloss.Center).
			Render(noSessions)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		statusLine,
		mainContent,
		hints,
		strip,
	)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return lipgloss.NewStyle().Render(
		itoa(n) + " " + plural)
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

// Package-level subscription channel, protected by a mutex.
// This is needed because Bubble Tea commands are functions that
// return messages — they can't carry state from the model directly
// in a safe way for long-running goroutines.
var (
	subMu sync.Mutex
	subCh <-chan []client.Session
)
