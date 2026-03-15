package ctlserver

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

// newTestState creates an isolated state manager using a temp directory.
func newTestState(t *testing.T) *state.Manager {
	return state.NewWithDir(t.TempDir())
}

// sendAndReceive sends a request over the connection and reads one response line.
func sendAndReceive(t *testing.T, conn net.Conn, req ctlRequest) ctlResponse {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response received")
	}

	var resp ctlResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v (raw: %s)", err, scanner.Text())
	}
	return resp
}

func setupHandler(t *testing.T) (*state.Manager, net.Conn) {
	t.Helper()
	st := newTestState(t)
	h := NewHandler(st)

	server, client := net.Pipe()
	go h.Handle(server)

	// Set a deadline so tests don't hang.
	client.SetDeadline(time.Now().Add(5 * time.Second))

	return st, client
}

// --- list ---

func TestHandleList(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path/project1", "default")
	st.RegisterSession("s2", "/path/project2", "default")

	resp := sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if resp.Sessions == nil {
		t.Fatal("expected sessions in response")
	}
	if len(resp.Sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(resp.Sessions))
	}
}

func TestHandleListEmpty(t *testing.T) {
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if len(resp.Sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(resp.Sessions))
	}
}

// --- toggle_autopilot ---

func TestHandleToggleAutopilot(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")

	// Cycle off → on.
	resp := sendAndReceive(t, conn, ctlRequest{Action: "toggle_autopilot", SessionID: "s1"})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true")
	}
	if resp.AutopilotMode != model.AutopilotOn {
		t.Errorf("mode = %q, want on", resp.AutopilotMode)
	}

	// Cycle on → yolo.
	resp = sendAndReceive(t, conn, ctlRequest{Action: "toggle_autopilot", SessionID: "s1"})
	if resp.AutopilotMode != model.AutopilotYolo {
		t.Errorf("mode = %q, want yolo", resp.AutopilotMode)
	}

	// Cycle yolo → off.
	resp = sendAndReceive(t, conn, ctlRequest{Action: "toggle_autopilot", SessionID: "s1"})
	if resp.AutopilotMode != model.AutopilotOff {
		t.Errorf("mode = %q, want off", resp.AutopilotMode)
	}
}

func TestHandleToggleAutopilotUnknownSession(t *testing.T) {
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{Action: "toggle_autopilot", SessionID: "nonexistent"})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for unknown session")
	}
}

// --- approve ---

func TestHandleApprove(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")
	ch := st.AddPending("s1", model.PendingTool{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "docker run ubuntu"},
	})

	resp := sendAndReceive(t, conn, ctlRequest{Action: "approve", SessionID: "s1"})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true for approve")
	}

	select {
	case d := <-ch:
		if d != model.DecisionAllow {
			t.Errorf("decision = %q, want allow", d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("no decision received")
	}
}

func TestHandleApproveNoPending(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")
	// No pending tool.

	resp := sendAndReceive(t, conn, ctlRequest{Action: "approve", SessionID: "s1"})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no pending tool")
	}
}

// --- reject ---

func TestHandleReject(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")
	ch := st.AddPending("s1", model.PendingTool{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "rm -rf /"},
	})

	resp := sendAndReceive(t, conn, ctlRequest{Action: "reject", SessionID: "s1"})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true for reject")
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

func TestHandleRejectNoPending(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")

	resp := sendAndReceive(t, conn, ctlRequest{Action: "reject", SessionID: "s1"})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no pending tool")
	}
}

// --- approve_all ---

func TestHandleApproveAll(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path1", "default")
	st.RegisterSession("s2", "/path2", "default")

	ch1 := st.AddPending("s1", model.PendingTool{
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/a.py"},
	})
	ch2 := st.AddPending("s2", model.PendingTool{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "rm file"},
	})

	resp := sendAndReceive(t, conn, ctlRequest{Action: "approve_all"})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true for approve_all")
	}

	// s1 (safe) should be approved.
	select {
	case d := <-ch1:
		if d != model.DecisionAllow {
			t.Errorf("s1 decision = %q, want allow", d)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("s1: no decision")
	}

	// s2 (destructive) should still be pending.
	select {
	case <-ch2:
		t.Error("s2 (destructive) should not be approved by approve_all")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestHandleApproveAllNoPending(t *testing.T) {
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{Action: "approve_all"})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when nothing to approve")
	}
}

// --- focus (no ghostty in test, always fails) ---

func TestHandleFocusNoGhosttyTab(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	st.RegisterSession("s1", "/path", "default")
	// No GhosttyTab set.

	resp := sendAndReceive(t, conn, ctlRequest{Action: "focus", SessionID: "s1"})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no ghostty tab")
	}
}

// --- Multiple actions in sequence ---

func TestMultipleActions(t *testing.T) {
	st, conn := setupHandler(t)
	defer conn.Close()

	// Register.
	st.RegisterSession("s1", "/path", "default")

	// List.
	resp := sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if len(resp.Sessions) != 1 {
		t.Fatalf("list: got %d sessions, want 1", len(resp.Sessions))
	}

	// Toggle.
	resp = sendAndReceive(t, conn, ctlRequest{Action: "toggle_autopilot", SessionID: "s1"})
	if resp.AutopilotMode != model.AutopilotOn {
		t.Errorf("toggle: mode = %q, want on", resp.AutopilotMode)
	}

	// List again — should reflect updated autopilot.
	resp = sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if len(resp.Sessions) != 1 {
		t.Fatalf("list 2: got %d sessions, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].AutopilotMode != model.AutopilotOn {
		t.Errorf("autopilot = %q, want on", resp.Sessions[0].AutopilotMode)
	}
}
