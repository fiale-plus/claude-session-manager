package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

func testModel(sessions []client.Session) Model {
	return Model{
		sessions:    sessions,
		selectedIdx: 0,
		connected:   true,
		width:       100,
		height:      30,
	}
}

func fourSessions() []client.Session {
	now := time.Now()
	return []client.Session{
		{SessionID: "s1", CWD: "/a", State: "running", PID: 1, LastActivity: &now},
		{SessionID: "s2", CWD: "/b", State: "idle", PID: 2},
		{SessionID: "s3", CWD: "/c", State: "waiting", PID: 3},
		{SessionID: "s4", CWD: "/d", State: "dead", PID: 4},
	}
}

// === Iteration 11: Home/End navigation ===

func TestHandleKey_HomeEnd(t *testing.T) {
	m := testModel(fourSessions())
	m.selectedIdx = 2

	// Home → first session
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m2.(Model).selectedIdx != 0 {
		t.Errorf("Home: selectedIdx = %d, want 0", m2.(Model).selectedIdx)
	}

	// End → last session
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m3.(Model).selectedIdx != 3 {
		t.Errorf("End: selectedIdx = %d, want 3", m3.(Model).selectedIdx)
	}
}

// === Iteration 12: Page up/down scrolling ===

func TestHandleKey_PageUpDown(t *testing.T) {
	m := testModel(fourSessions())
	m.scrollOffset = 10

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m2.(Model).scrollOffset >= 10 {
		t.Errorf("PgUp: scrollOffset = %d, want < 10", m2.(Model).scrollOffset)
	}

	m3 := testModel(fourSessions())
	m3.scrollOffset = 0
	m4, _ := m3.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m4.(Model).scrollOffset <= 0 {
		t.Errorf("PgDown: scrollOffset = %d, want > 0", m4.(Model).scrollOffset)
	}
}

// === Iteration 13: Navigation bounds ===

func TestHandleKey_NavigationBounds(t *testing.T) {
	m := testModel(fourSessions())

	// Left at 0 stays at 0
	m.selectedIdx = 0
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m2.(Model).selectedIdx != 0 {
		t.Errorf("Left at 0: selectedIdx = %d, want 0", m2.(Model).selectedIdx)
	}

	// Right at last stays at last
	m.selectedIdx = 3
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m3.(Model).selectedIdx != 3 {
		t.Errorf("Right at 3: selectedIdx = %d, want 3", m3.(Model).selectedIdx)
	}

	// Up at scroll 0 stays at 0
	m.scrollOffset = 0
	m4, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m4.(Model).scrollOffset != 0 {
		t.Errorf("Up at 0: scrollOffset = %d, want 0", m4.(Model).scrollOffset)
	}
}

// === Iteration 14: View with zero sessions ===

func TestView_NoSessions(t *testing.T) {
	m := testModel(nil)
	view := m.View()
	if view == "" {
		t.Error("View with no sessions should produce output")
	}
	if !strings.Contains(view, "session") {
		t.Error("empty view should mention sessions")
	}
}

// === Iteration 15: View with disconnected state ===

func TestView_Disconnected(t *testing.T) {
	m := testModel(fourSessions())
	m.connected = false
	view := m.View()
	if !strings.Contains(view, "disconnected") {
		t.Error("disconnected state should show 'disconnected'")
	}
}

// === Iteration 16: Session selection stability ===

func TestSessionSelection_Stable(t *testing.T) {
	m := testModel(fourSessions())
	m.selectedIdx = 2
	m.selectedSID = "s3"

	// Simulate session update — order changes, s3 moves to index 0.
	newSessions := []client.Session{
		{SessionID: "s3", CWD: "/c", State: "waiting", PID: 3},
		{SessionID: "s1", CWD: "/a", State: "running", PID: 1},
		{SessionID: "s4", CWD: "/d", State: "dead", PID: 4},
	}
	m2, _ := m.Update(stateMsg{Sessions: newSessions})
	model := m2.(Model)
	if model.selectedSID != "s3" {
		t.Errorf("selected SID = %q, want 's3'", model.selectedSID)
	}
	if model.selectedIdx != 0 {
		t.Errorf("after reorder, selectedIdx = %d, want 0", model.selectedIdx)
	}
}

// === Iteration 17: View at extreme dimensions ===

func TestView_ExtremeDimensions(t *testing.T) {
	sessions := fourSessions()

	dims := []struct {
		w, h int
		name string
	}{
		{20, 8, "tiny"},
		{200, 50, "huge"},
		{30, 6, "cramped"},
		{150, 10, "wide+short"},
		{25, 40, "narrow+tall"},
	}

	for _, d := range dims {
		t.Run(d.name, func(t *testing.T) {
			m := testModel(sessions)
			m.width = d.w
			m.height = d.h
			// Must not panic
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > d.h {
				t.Errorf("%s: %d lines exceed height %d", d.name, len(lines), d.h)
			}
		})
	}
}

// === Iteration 18: Flash message display ===

func TestView_FlashMessage(t *testing.T) {
	m := testModel(fourSessions())
	m.flash = "approve sent"
	m.flashStyle = lipgloss.NewStyle().Foreground(colorRunning)

	view := m.View()
	if !strings.Contains(view, "approve sent") {
		t.Error("flash message should be visible in view")
	}
}

// === Iteration 19: Queue panel ===

func TestRenderQueue_NoPanic(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", ProjectName: "proj",
			PendingTools: []client.PendingTool{
				{ToolName: "Bash", ToolInput: map[string]any{"command": "ls"}, Safety: "safe"},
				{ToolName: "Edit", ToolInput: map[string]any{"file_path": "/foo.go"}, Safety: "destructive"},
			}},
		{SessionID: "s2", ProjectName: "other",
			PendingTools: []client.PendingTool{
				{ToolName: "Read", ToolInput: map[string]any{"file_path": "/bar.go"}, Safety: "safe"},
			}},
	}

	for _, dims := range []struct{ w, h int }{
		{100, 20},
		{40, 8},
		{25, 5},
	} {
		// Must not panic
		out := renderQueue(sessions, dims.w, dims.h)
		if out == "" {
			t.Errorf("renderQueue at %dx%d produced empty output", dims.w, dims.h)
		}
	}
}

// === Iteration 20: All states in View ===

func TestView_AllStates(t *testing.T) {
	now := time.Now()
	sessions := []client.Session{
		{SessionID: "run1", State: "running", PID: 1, CWD: "/a", LastActivity: &now,
			AutopilotMode: "on", GitBranch: "feature/test",
			Activities: []client.Activity{
				{Timestamp: now, ActivityType: "tool_use", Summary: "Edit: main.go"},
			},
			LastText: "Working on feature"},
		{SessionID: "wait1", State: "waiting", PID: 2, CWD: "/b",
			PendingTools: []client.PendingTool{
				{ToolName: "Bash", ToolInput: map[string]any{"command": "rm -rf /"}, Safety: "destructive"},
			}},
		{SessionID: "idle1", State: "idle", PID: 3, CWD: "/c"},
		{SessionID: "dead1", State: "dead", PID: 4, CWD: "/d", AutopilotMode: "yolo"},
	}

	for _, selIdx := range []int{0, 1, 2, 3} {
		m := testModel(sessions)
		m.selectedIdx = selIdx
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > m.height {
			t.Errorf("sel=%d: %d lines exceed height %d", selIdx, len(lines), m.height)
		}
		if view == "" {
			t.Errorf("sel=%d: empty view", selIdx)
		}
	}
}
