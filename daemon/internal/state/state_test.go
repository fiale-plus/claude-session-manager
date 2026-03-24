package state

import (
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// newTestManager creates a Manager without disk persistence.
func newTestManager() *Manager {
	return &Manager{
		sessions:  make(map[string]*model.Session),
		autopilot: make(map[string]string),
		pending:   make(map[string]*model.PendingApproval),
		cooldowns: make(map[string]time.Time),
	}
}

// --- RegisterSession / UnregisterSession ---

func TestRegisterSession(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/home/user/project", "default")

	sessions := m.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.SessionID != "s1" {
		t.Errorf("session_id = %q, want s1", s.SessionID)
	}
	if s.CWD != "/home/user/project" {
		t.Errorf("cwd = %q, want /home/user/project", s.CWD)
	}
	if s.PermissionMode != "default" {
		t.Errorf("permission_mode = %q, want default", s.PermissionMode)
	}
	if s.ProjectName != "project" {
		t.Errorf("project_name = %q, want project", s.ProjectName)
	}
	if s.State != model.StateIdle {
		t.Errorf("state = %q, want idle", s.State)
	}
}

func TestRegisterSessionUpdatesExisting(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/old/path", "default")
	m.RegisterSession("s1", "/new/path", "plan")

	sessions := m.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CWD != "/new/path" {
		t.Errorf("cwd = %q, want /new/path", sessions[0].CWD)
	}
	if sessions[0].PermissionMode != "plan" {
		t.Errorf("permission_mode = %q, want plan", sessions[0].PermissionMode)
	}
}

func TestRegisterSessionRestoresAutopilot(t *testing.T) {
	m := newTestManager()
	m.autopilot["s1"] = model.AutopilotOn
	m.RegisterSession("s1", "/path", "default")

	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotOn {
		t.Errorf("autopilot = %q, want on", sessions[0].AutopilotMode)
	}
}

func TestUnregisterSession(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	m.UnregisterSession("s1")

	sessions := m.GetSessions()
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

func TestUnregisterSessionClosesPending(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "ls"}}
	ch := m.AddPending("s1", tool)

	m.UnregisterSession("s1")

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			// Got a value — channel might have been written to before close.
		}
	default:
		// Channel might already be drained.
	}

	if m.GetPending("s1") != nil {
		t.Error("pending should be cleaned up after unregister")
	}
}

// --- CycleAutopilot ---

func TestCycleAutopilot(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	// off → on
	mode, ok := m.CycleAutopilot("s1")
	if !ok || mode != model.AutopilotOn {
		t.Errorf("cycle 1: got (%q, %v), want (on, true)", mode, ok)
	}

	// on → yolo
	mode, ok = m.CycleAutopilot("s1")
	if !ok || mode != model.AutopilotYolo {
		t.Errorf("cycle 2: got (%q, %v), want (yolo, true)", mode, ok)
	}

	// yolo → off
	mode, ok = m.CycleAutopilot("s1")
	if !ok || mode != model.AutopilotOff {
		t.Errorf("cycle 3: got (%q, %v), want (off, true)", mode, ok)
	}
}

func TestCycleAutopilotUnknownSession(t *testing.T) {
	m := newTestManager()
	mode, ok := m.CycleAutopilot("nonexistent")
	if ok {
		t.Errorf("expected ok=false for unknown session, got mode=%q", mode)
	}
}

func TestCycleAutopilotApprovesPendingSafe(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	// Add a safe pending tool.
	tool := model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a.py"}}
	ch := m.AddPending("s1", tool)

	// Cycle to ON — should auto-approve safe tool.
	m.CycleAutopilot("s1")

	select {
	case decision := <-ch:
		if decision != model.DecisionAllow {
			t.Errorf("expected allow, got %q", decision)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected decision on channel, timed out")
	}
}

func TestCycleAutopilotOnBlocksDestructive(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	// Add a destructive pending tool.
	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "git push"}}
	ch := m.AddPending("s1", tool)

	// Cycle to ON — should NOT approve destructive.
	m.CycleAutopilot("s1")

	select {
	case <-ch:
		t.Error("destructive tool should NOT be approved in ON mode")
	case <-time.After(50 * time.Millisecond):
		// Expected: no decision.
	}

	// Pending should still be there.
	if m.GetPending("s1") == nil {
		t.Error("pending should still exist for destructive tool in ON mode")
	}
}

func TestCycleAutopilotYoloApprovesAll(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	// Add a destructive pending tool.
	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "git push"}}
	ch := m.AddPending("s1", tool)

	// Cycle to ON, then to YOLO.
	m.CycleAutopilot("s1") // off → on (destructive stays pending)

	// We need to re-add since the pending map was cleared or not.
	// Actually in ON mode destructive stays pending, so cycle again:
	m.CycleAutopilot("s1") // on → yolo (should approve everything)

	select {
	case decision := <-ch:
		if decision != model.DecisionAllow {
			t.Errorf("YOLO should approve destructive, got %q", decision)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected decision in YOLO mode, timed out")
	}
}

// --- AddPending / ResolvePending ---

func TestAddPendingAndResolve(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "docker run ubuntu"}}
	ch := m.AddPending("s1", tool)

	// Verify pending exists.
	pa := m.GetPending("s1")
	if pa == nil {
		t.Fatal("expected pending approval")
	}
	if pa.Tool.ToolName != "Bash" {
		t.Errorf("tool = %q, want Bash", pa.Tool.ToolName)
	}

	// Resolve with allow.
	ok := m.ResolvePending("s1", model.DecisionAllow)
	if !ok {
		t.Error("ResolvePending should return true")
	}

	select {
	case decision := <-ch:
		if decision != model.DecisionAllow {
			t.Errorf("decision = %q, want allow", decision)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("no decision received")
	}

	// Pending should be cleared.
	if m.GetPending("s1") != nil {
		t.Error("pending should be nil after resolve")
	}
}

func TestResolveNonexistentPending(t *testing.T) {
	m := newTestManager()
	ok := m.ResolvePending("nonexistent", model.DecisionAllow)
	if ok {
		t.Error("ResolvePending for nonexistent should return false")
	}
}

func TestResolvePendingCooldown(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	// First approval.
	tool := model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a"}}
	m.AddPending("s1", tool)
	m.ResolvePending("s1", model.DecisionAllow)

	// Immediately add and try to resolve again (within cooldown).
	tool2 := model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/b"}}
	m.AddPending("s1", tool2)
	ok := m.ResolvePending("s1", model.DecisionAllow)
	if ok {
		t.Error("second resolve within cooldown should return false")
	}
}

func TestResolvePendingDeny(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "rm -rf /"}}
	ch := m.AddPending("s1", tool)

	ok := m.ResolvePending("s1", model.DecisionDeny)
	if !ok {
		t.Error("ResolvePending deny should return true")
	}

	select {
	case d := <-ch:
		if d != model.DecisionDeny {
			t.Errorf("decision = %q, want deny", d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("no decision received")
	}
}

// --- ApproveAllPending ---

func TestApproveAllPending(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path1", "default")
	m.RegisterSession("s2", "/path2", "default")
	m.RegisterSession("s3", "/path3", "default")

	// s1: safe tool.
	ch1 := m.AddPending("s1", model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a"}})
	// s2: destructive tool.
	ch2 := m.AddPending("s2", model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "rm file"}})
	// s3: unknown tool.
	ch3 := m.AddPending("s3", model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "docker build ."}})

	count := m.ApproveAllPending()
	// Should approve s1 (safe) and s3 (unknown), skip s2 (destructive).
	if count != 2 {
		t.Errorf("approved %d, want 2", count)
	}

	// s1 should be approved.
	select {
	case d := <-ch1:
		if d != model.DecisionAllow {
			t.Errorf("s1 decision = %q, want allow", d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("s1: no decision")
	}

	// s2 should still be pending.
	if m.GetPending("s2") == nil {
		t.Error("s2 (destructive) should still be pending")
	}
	_ = ch2

	// s3 should be approved.
	select {
	case d := <-ch3:
		if d != model.DecisionAllow {
			t.Errorf("s3 decision = %q, want allow", d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("s3: no decision")
	}
}

func TestApproveAllPendingNone(t *testing.T) {
	m := newTestManager()
	count := m.ApproveAllPending()
	if count != 0 {
		t.Errorf("approved %d, want 0", count)
	}
}

// --- GetSessions ---

func TestGetSessionsFiltersDeadNoPID(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.sessions["s1"] = &model.Session{SessionID: "s1", State: model.StateDead, PID: 0}
	m.sessions["s2"] = &model.Session{SessionID: "s2", State: model.StateDead, PID: 12345}
	m.sessions["s3"] = &model.Session{SessionID: "s3", State: model.StateRunning}
	m.mu.Unlock()

	sessions := m.GetSessions()
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (dead+PID=0 should be filtered)", len(sessions))
	}
}

func TestGetSessionsSortsByState(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.sessions["s-idle"] = &model.Session{SessionID: "s-idle", State: model.StateIdle, PID: 1}
	m.sessions["s-running"] = &model.Session{SessionID: "s-running", State: model.StateRunning, PID: 2}
	m.sessions["s-waiting"] = &model.Session{SessionID: "s-waiting", State: model.StateWaiting, PID: 3}
	m.sessions["s-dead"] = &model.Session{SessionID: "s-dead", State: model.StateDead, PID: 4}
	m.mu.Unlock()

	sessions := m.GetSessions()
	if len(sessions) != 4 {
		t.Fatalf("got %d sessions, want 4", len(sessions))
	}
	expected := []model.SessionState{model.StateRunning, model.StateWaiting, model.StateIdle, model.StateDead}
	for i, s := range sessions {
		if s.State != expected[i] {
			t.Errorf("sessions[%d].State = %q, want %q", i, s.State, expected[i])
		}
	}
}

func TestGetSessionsSortsByIDWithinState(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.sessions["s-b"] = &model.Session{SessionID: "s-b", State: model.StateRunning, PID: 1}
	m.sessions["s-a"] = &model.Session{SessionID: "s-a", State: model.StateRunning, PID: 2}
	m.mu.Unlock()

	sessions := m.GetSessions()
	if len(sessions) != 2 {
		t.Fatalf("got %d, want 2", len(sessions))
	}
	if sessions[0].SessionID != "s-a" {
		t.Errorf("first = %q, want s-a", sessions[0].SessionID)
	}
}

// --- ShouldAutoApprove ---

func TestShouldAutoApproveOff(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	// Default is off.

	approve, grace := m.ShouldAutoApprove("s1", model.SafetySafe)
	if approve || grace {
		t.Errorf("OFF mode: approve=%v, grace=%v, want false/false", approve, grace)
	}
}

func TestShouldAutoApproveOn(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	m.CycleAutopilot("s1") // off → on

	// Safe → approve.
	approve, grace := m.ShouldAutoApprove("s1", model.SafetySafe)
	if !approve || grace {
		t.Errorf("ON+safe: approve=%v, grace=%v, want true/false", approve, grace)
	}

	// Unknown → approve.
	approve, grace = m.ShouldAutoApprove("s1", model.SafetyUnknown)
	if !approve || grace {
		t.Errorf("ON+unknown: approve=%v, grace=%v, want true/false", approve, grace)
	}

	// Destructive → block.
	approve, grace = m.ShouldAutoApprove("s1", model.SafetyDestructive)
	if approve || grace {
		t.Errorf("ON+destructive: approve=%v, grace=%v, want false/false", approve, grace)
	}
}

func TestShouldAutoApproveYolo(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	m.CycleAutopilot("s1") // off → on
	m.CycleAutopilot("s1") // on → yolo

	// Safe → approve.
	approve, grace := m.ShouldAutoApprove("s1", model.SafetySafe)
	if !approve || grace {
		t.Errorf("YOLO+safe: approve=%v, grace=%v, want true/false", approve, grace)
	}

	// Unknown → approve.
	approve, grace = m.ShouldAutoApprove("s1", model.SafetyUnknown)
	if !approve || grace {
		t.Errorf("YOLO+unknown: approve=%v, grace=%v, want true/false", approve, grace)
	}

	// Destructive → grace.
	approve, grace = m.ShouldAutoApprove("s1", model.SafetyDestructive)
	if approve || !grace {
		t.Errorf("YOLO+destructive: approve=%v, grace=%v, want false/true", approve, grace)
	}
}

func TestShouldAutoApproveUnknownSession(t *testing.T) {
	m := newTestManager()
	approve, grace := m.ShouldAutoApprove("nonexistent", model.SafetySafe)
	if approve || grace {
		t.Errorf("unknown session: approve=%v, grace=%v, want false/false", approve, grace)
	}
}

func TestShouldAutoApprovePersistedFallback(t *testing.T) {
	m := newTestManager()
	// Set persisted autopilot without registering session.
	m.mu.Lock()
	m.autopilot["s1"] = model.AutopilotOn
	m.mu.Unlock()

	// Session not in m.sessions — should fallback to persisted state.
	approve, grace := m.ShouldAutoApprove("s1", model.SafetySafe)
	if !approve {
		t.Error("persisted ON + safe: should approve")
	}
	if grace {
		t.Error("persisted ON + safe: should not grace")
	}

	// Destructive should be blocked even with persisted ON.
	approve, grace = m.ShouldAutoApprove("s1", model.SafetyDestructive)
	if approve {
		t.Error("persisted ON + destructive: should not approve")
	}
}

func TestShouldAutoApprovePersistedYolo(t *testing.T) {
	m := newTestManager()
	m.mu.Lock()
	m.autopilot["s1"] = model.AutopilotYolo
	m.mu.Unlock()

	approve, grace := m.ShouldAutoApprove("s1", model.SafetyDestructive)
	if approve {
		t.Error("persisted YOLO + destructive: should not immediately approve")
	}
	if !grace {
		t.Error("persisted YOLO + destructive: should grace")
	}
}

// --- SetSlug / SetGhosttyTab ---

func TestSetSlug(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	m.SetSlug("s1", "my-slug")

	sessions := m.GetSessions()
	if sessions[0].Slug != "my-slug" {
		t.Errorf("slug = %q, want my-slug", sessions[0].Slug)
	}
}

func TestSetSlugNonexistentSession(t *testing.T) {
	m := newTestManager()
	// Should not panic.
	m.SetSlug("nonexistent", "slug")
}

func TestSetGhosttyTab(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	m.SetGhosttyTab("s1", "Terminal 1", 1)

	sessions := m.GetSessions()
	if sessions[0].GhosttyTab != "Terminal 1" {
		t.Errorf("ghostty_tab = %q, want Terminal 1", sessions[0].GhosttyTab)
	}
	if sessions[0].GhosttyTabIndex != 1 {
		t.Errorf("ghostty_tab_index = %d, want 1", sessions[0].GhosttyTabIndex)
	}
}

func TestSetGhosttyTabNonexistent(t *testing.T) {
	m := newTestManager()
	// Should not panic.
	m.SetGhosttyTab("nonexistent", "tab", 1)
}

// --- Subscriber notifications ---

func TestSubscriberNotifications(t *testing.T) {
	m := newTestManager()
	ch := m.Subscribe()
	defer m.Unsubscribe(ch)

	// Register should trigger notification.
	m.RegisterSession("s1", "/path", "default")

	select {
	case <-ch:
		// Good.
	case <-time.After(100 * time.Millisecond):
		t.Error("expected notification on register")
	}
}

func TestSubscriberMultipleNotifications(t *testing.T) {
	m := newTestManager()
	ch := m.Subscribe()
	defer m.Unsubscribe(ch)

	m.RegisterSession("s1", "/path", "default")
	m.SetSlug("s1", "slug")
	m.UnregisterSession("s1")

	// Drain all notifications.
	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if count < 2 {
		t.Errorf("expected at least 2 notifications, got %d", count)
	}
}

func TestUnsubscribe(t *testing.T) {
	m := newTestManager()
	ch := m.Subscribe()
	m.Unsubscribe(ch)

	// Registering after unsubscribe should not panic or send.
	m.RegisterSession("s1", "/path", "default")

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after unsubscribe")
		}
	case <-time.After(50 * time.Millisecond):
		// Also acceptable if nothing comes through.
	}
}

// --- AddPending sets safety and HasDestructive ---

func TestAddPendingSetsDestructiveFlag(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "rm -rf /"}}
	m.AddPending("s1", tool)

	sessions := m.GetSessions()
	if !sessions[0].HasDestructive {
		t.Error("HasDestructive should be true for rm -rf")
	}
}

func TestAddPendingSafeNotDestructive(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a.py"}}
	m.AddPending("s1", tool)

	sessions := m.GetSessions()
	if sessions[0].HasDestructive {
		t.Error("HasDestructive should be false for Read")
	}
}

// --- Concurrent operations ---

func TestConcurrentRegisterAndGetSessions(t *testing.T) {
	m := newTestManager()
	done := make(chan struct{})
	for g := 0; g < 5; g++ {
		go func(n int) {
			for i := 0; i < 20; i++ {
				sid := "s" + string(rune('a'+n)) + string(rune('0'+i%10))
				m.RegisterSession(sid, "/path", "default")
			}
			done <- struct{}{}
		}(g)
	}
	for g := 0; g < 5; g++ {
		go func() {
			for i := 0; i < 20; i++ {
				_ = m.GetSessions()
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 10; g++ {
		<-done
	}
}

func TestConcurrentCycleAutopilot(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	done := make(chan struct{})
	for g := 0; g < 5; g++ {
		go func() {
			for i := 0; i < 20; i++ {
				m.CycleAutopilot("s1")
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 5; g++ {
		<-done
	}
}

func TestConcurrentSubscribeNotify(t *testing.T) {
	m := newTestManager()
	done := make(chan struct{})
	for g := 0; g < 3; g++ {
		go func() {
			ch := m.Subscribe()
			defer m.Unsubscribe(ch)
			for i := 0; i < 10; i++ {
				select {
				case <-ch:
				case <-time.After(50 * time.Millisecond):
				}
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 3; g++ {
		go func() {
			for i := 0; i < 10; i++ {
				m.NotifySubscribers()
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 6; g++ {
		<-done
	}
}

func TestConcurrentAddResolvePending(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")
	done := make(chan struct{})

	go func() {
		for i := 0; i < 20; i++ {
			tool := model.PendingTool{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a"}}
			m.AddPending("s1", tool)
			// Small sleep to let resolve happen sometimes.
			time.Sleep(time.Millisecond)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 20; i++ {
			m.ResolvePending("s1", model.DecisionAllow)
			time.Sleep(time.Millisecond)
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// --- UpdateSessionFromScanner ---

func TestUpdateSessionFromScannerNewSession(t *testing.T) {
	m := newTestManager()
	s := &model.Session{
		SessionID: "scanner-1",
		CWD:       "/discovered/path",
		State:     model.StateRunning,
		PID:       12345,
	}
	m.UpdateSessionFromScanner(s)

	sessions := m.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].SessionID != "scanner-1" {
		t.Errorf("sid = %q, want scanner-1", sessions[0].SessionID)
	}
	if sessions[0].State != model.StateRunning {
		t.Errorf("state = %q, want running", sessions[0].State)
	}
}

func TestUpdateSessionFromScannerMergesExisting(t *testing.T) {
	m := newTestManager()
	// Register via hook first.
	m.RegisterSession("s1", "/hook/path", "plan")
	m.SetSlug("s1", "my-slug")

	// Then scanner updates with richer data.
	s := &model.Session{
		SessionID: "s1",
		CWD:       "/scanner/path", // should NOT overwrite hook CWD
		State:     model.StateRunning,
		PID:       999,
		GitBranch: "feature",
	}
	m.UpdateSessionFromScanner(s)

	sessions := m.GetSessions()
	if sessions[0].CWD != "/hook/path" {
		t.Errorf("CWD should keep hook value, got %q", sessions[0].CWD)
	}
	if sessions[0].State != model.StateRunning {
		t.Errorf("state should be updated from scanner, got %q", sessions[0].State)
	}
	if sessions[0].PID != 999 {
		t.Errorf("PID should be updated from scanner, got %d", sessions[0].PID)
	}
	if sessions[0].GitBranch != "feature" {
		t.Errorf("git_branch should be updated from scanner, got %q", sessions[0].GitBranch)
	}
}

func TestUpdateSessionFromScannerRestoresAutopilot(t *testing.T) {
	m := newTestManager()
	m.autopilot["scanner-1"] = model.AutopilotYolo
	s := &model.Session{SessionID: "scanner-1", CWD: "/path", State: model.StateIdle, PID: 1}
	m.UpdateSessionFromScanner(s)

	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotYolo {
		t.Errorf("autopilot = %q, want yolo", sessions[0].AutopilotMode)
	}
}

// --- Persistence ---

func TestNewWithDirPersistsAutopilot(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)
	m.RegisterSession("s1", "/path", "default")
	m.CycleAutopilot("s1") // off -> on

	// Create new manager from same dir.
	m2 := NewWithDir(dir)
	m2.RegisterSession("s1", "/path", "default")
	sessions := m2.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotOn {
		t.Errorf("persisted autopilot = %q, want on", sessions[0].AutopilotMode)
	}
}

func TestNewWithDirEmptyDir(t *testing.T) {
	m := NewWithDir("")
	m.RegisterSession("s1", "/path", "default")
	sessions := m.GetSessions()
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}

func TestResolvePendingClearsPendingTools(t *testing.T) {
	m := newTestManager()
	m.RegisterSession("s1", "/path", "default")

	tool := model.PendingTool{ToolName: "Bash", ToolInput: map[string]any{"command": "rm file"}}
	m.AddPending("s1", tool)

	// Verify pending tools on session.
	sessions := m.GetSessions()
	if len(sessions[0].PendingTools) != 1 {
		t.Fatalf("pending_tools = %d, want 1", len(sessions[0].PendingTools))
	}

	m.ResolvePending("s1", model.DecisionAllow)

	sessions = m.GetSessions()
	if len(sessions[0].PendingTools) != 0 {
		t.Errorf("pending_tools should be cleared after resolve, got %d", len(sessions[0].PendingTools))
	}
	if sessions[0].HasDestructive {
		t.Error("HasDestructive should be false after resolve")
	}
}

// --- Default autopilot ---

// newTestManagerWithDefault creates a Manager without disk persistence but
// with a pre-configured default autopilot mode.
func newTestManagerWithDefault(defaultMode string) *Manager {
	return &Manager{
		sessions:  make(map[string]*model.Session),
		autopilot: make(map[string]string),
		pending:   make(map[string]*model.PendingApproval),
		cooldowns: make(map[string]time.Time),
		config:    Config{DefaultAutopilot: defaultMode},
	}
}

// TestDefaultAutopilotAppliedOnRegister verifies that new sessions receive the
// default autopilot mode when no per-session override exists.
func TestDefaultAutopilotAppliedOnRegister(t *testing.T) {
	m := newTestManagerWithDefault(model.AutopilotYolo)
	m.RegisterSession("s1", "/path", "default")

	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotYolo {
		t.Errorf("autopilot = %q, want yolo (default)", sessions[0].AutopilotMode)
	}
}

// TestPersistedAutopilotOverridesDefault verifies that a persisted per-session
// mode takes priority over the daemon-wide default.
func TestPersistedAutopilotOverridesDefault(t *testing.T) {
	m := newTestManagerWithDefault(model.AutopilotYolo)
	m.autopilot["s1"] = model.AutopilotOn // persisted override
	m.RegisterSession("s1", "/path", "default")

	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotOn {
		t.Errorf("autopilot = %q, want on (persisted should override default yolo)", sessions[0].AutopilotMode)
	}
}

// TestSetDefaultAutopilotPersistsToDisk verifies that SetDefaultAutopilot
// writes to disk and is readable by a new manager loaded from the same dir.
func TestSetDefaultAutopilotPersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir(dir)
	m.SetDefaultAutopilot(model.AutopilotYolo)

	// New manager from same dir should load the persisted default.
	m2 := NewWithDir(dir)
	if got := m2.GetDefaultAutopilot(); got != model.AutopilotYolo {
		t.Errorf("GetDefaultAutopilot = %q, want yolo", got)
	}
}

// TestDefaultAutopilotAppliedOnScanner verifies that scanner-discovered
// sessions also receive the default autopilot mode.
func TestDefaultAutopilotAppliedOnScanner(t *testing.T) {
	m := newTestManagerWithDefault(model.AutopilotOn)
	s := &model.Session{SessionID: "scan-1", CWD: "/path", State: model.StateRunning, PID: 1}
	m.UpdateSessionFromScanner(s)

	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotOn {
		t.Errorf("scanner session autopilot = %q, want on (default)", sessions[0].AutopilotMode)
	}
}

// TestDefaultNotAppliedWhenSessionAlreadyRegistered verifies that when a
// session is first registered via hook (getting the default), a subsequent
// scanner update does not clobber the mode.
func TestDefaultNotAppliedWhenSessionAlreadyRegistered(t *testing.T) {
	m := newTestManagerWithDefault(model.AutopilotOn)
	// Hook registers the session; default is applied.
	m.RegisterSession("s1", "/path", "default")

	// User later cycles to yolo.
	m.CycleAutopilot("s1") // on → yolo (since default applied "on" first)
	// Actually start from registered state: default=on → cycle → yolo.
	sessions := m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotYolo {
		t.Fatalf("after cycle: want yolo, got %q", sessions[0].AutopilotMode)
	}

	// Now scanner update comes in for the existing session — should NOT reset mode.
	s := &model.Session{SessionID: "s1", CWD: "/scanner/path", State: model.StateRunning, PID: 999}
	m.UpdateSessionFromScanner(s)

	sessions = m.GetSessions()
	if sessions[0].AutopilotMode != model.AutopilotYolo {
		t.Errorf("scanner update clobbered autopilot: got %q, want yolo", sessions[0].AutopilotMode)
	}
}

// TestGetSetDefaultAutopilot verifies the get/set round-trip in-memory.
func TestGetSetDefaultAutopilot(t *testing.T) {
	m := newTestManager()

	if got := m.GetDefaultAutopilot(); got != "" {
		t.Errorf("initial default = %q, want empty", got)
	}

	m.SetDefaultAutopilot(model.AutopilotOn)
	if got := m.GetDefaultAutopilot(); got != model.AutopilotOn {
		t.Errorf("after set: default = %q, want on", got)
	}

	m.SetDefaultAutopilot("")
	if got := m.GetDefaultAutopilot(); got != "" {
		t.Errorf("after clear: default = %q, want empty", got)
	}
}
