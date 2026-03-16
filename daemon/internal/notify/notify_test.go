package notify

import (
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

func TestNew(t *testing.T) {
	n := New()
	if n == nil {
		t.Fatal("New() should not return nil")
	}
	if n.prevState == nil || n.prevDestr == nil || n.lastNotified == nil {
		t.Error("New() should initialize all maps")
	}
}

func TestCheck_NoTransition(t *testing.T) {
	n := New()
	sessions := []model.Session{
		{SessionID: "s1", State: model.StateRunning, ProjectName: "test"},
	}
	// First call sets the baseline state.
	n.Check(sessions)
	// Second call with same state should not trigger notification.
	n.Check(sessions)
}

func TestCheck_RunningToWaitingTransition(t *testing.T) {
	n := New()
	// Set baseline.
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateRunning, ProjectName: "test"},
	})
	// Transition to waiting.
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateWaiting, ProjectName: "test"},
	})
	// State should be updated.
	if n.prevState["s1"] != model.StateWaiting {
		t.Errorf("prevState = %q, want waiting", n.prevState["s1"])
	}
}

func TestCheck_DestructiveFlag(t *testing.T) {
	n := New()
	// Baseline.
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateRunning, AutopilotMode: model.AutopilotOn,
			HasDestructive: false},
	})
	// Destructive tool appears.
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateRunning, AutopilotMode: model.AutopilotOn,
			HasDestructive: true},
	})
	if !n.prevDestr["s1"] {
		t.Error("prevDestr should be true after destructive flag set")
	}
}

func TestCheck_RateLimiting(t *testing.T) {
	n := New()
	// Simulate a notification.
	n.lastNotified["s1"] = time.Now()
	// maybeNotify with recent last notification should be skipped.
	n.maybeNotify("s1", time.Now(), "test", "msg")
	// No panic = pass. We can't easily verify notification wasn't sent without mocking.
}

func TestCheck_EmptySessions(t *testing.T) {
	n := New()
	// Should not panic with empty sessions.
	n.Check(nil)
	n.Check([]model.Session{})
}

func TestCheck_UsesSlugForName(t *testing.T) {
	n := New()
	sessions := []model.Session{
		{SessionID: "s1", State: model.StateRunning, Slug: "my-slug"},
	}
	n.Check(sessions)
	// No assertion on notification text since we can't capture it,
	// but verifying no panic when ProjectName is empty and Slug is used.
}

func TestCheck_FallsBackToSessionID(t *testing.T) {
	n := New()
	sessions := []model.Session{
		{SessionID: "s1", State: model.StateRunning},
	}
	n.Check(sessions)
	// No panic when all name fields are empty.
}

func TestCheck_MultipleSessions(t *testing.T) {
	n := New()
	sessions := []model.Session{
		{SessionID: "s1", State: model.StateRunning, ProjectName: "a"},
		{SessionID: "s2", State: model.StateIdle, ProjectName: "b"},
		{SessionID: "s3", State: model.StateWaiting, ProjectName: "c"},
	}
	n.Check(sessions)
	if len(n.prevState) != 3 {
		t.Errorf("prevState has %d entries, want 3", len(n.prevState))
	}
}

func TestCheck_DestructiveWithoutAutopilot(t *testing.T) {
	n := New()
	// Destructive flag but autopilot off — should not trigger destructive notification.
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateRunning, AutopilotMode: model.AutopilotOff,
			HasDestructive: false},
	})
	n.Check([]model.Session{
		{SessionID: "s1", State: model.StateRunning, AutopilotMode: model.AutopilotOff,
			HasDestructive: true},
	})
	// No destructive notification should fire with autopilot off.
}
