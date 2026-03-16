package ctlserver

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/pr"
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
	h := NewHandler(st, nil)

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

// --- PR handler setup ---

func setupHandlerWithPR(t *testing.T) (*state.Manager, *pr.Poller, net.Conn) {
	t.Helper()
	st := newTestState(t)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	prPoll := pr.NewPoller(storePath, nil)
	h := NewHandler(st, prPoll)

	server, client := net.Pipe()
	go h.Handle(server)
	client.SetDeadline(time.Now().Add(5 * time.Second))

	return st, prPoll, client
}

// --- add_pr ---

func TestHandleAddPR(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "add_pr",
		PRURL:  "https://github.com/octocat/hello-world/pull/42",
	})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true for add_pr")
	}
}

func TestHandleAddPR_InvalidURL(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "add_pr",
		PRURL:  "not a valid url",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for invalid URL")
	}
}

func TestHandleAddPR_NoPRPoller(t *testing.T) {
	// Use handler without PR poller.
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "add_pr",
		PRURL:  "https://github.com/owner/repo/pull/1",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no PR poller")
	}
}

// --- remove_pr ---

func TestHandleRemovePR(t *testing.T) {
	_, prPoll, conn := setupHandlerWithPR(t)
	defer conn.Close()

	// First add a PR directly.
	prPoll.Add("owner", "repo", 1)

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "remove_pr",
		PRKey:  "owner/repo#1",
	})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true for remove_pr")
	}

	// Verify it was removed.
	all := prPoll.GetAll()
	if len(all) != 0 {
		t.Errorf("after remove: got %d PRs, want 0", len(all))
	}
}

func TestHandleRemovePR_InvalidKey(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	// No # separator.
	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "remove_pr",
		PRKey:  "invalid-key",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for invalid key (no #)")
	}
}

func TestHandleRemovePR_InvalidKeyNoSlash(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	// # present but no / in owner/repo part.
	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "remove_pr",
		PRKey:  "ownerrepo#1",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for invalid key (no /)")
	}
}

func TestHandleRemovePR_NoPRPoller(t *testing.T) {
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "remove_pr",
		PRKey:  "owner/repo#1",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no PR poller")
	}
}

// --- cycle_pr_autopilot ---

func TestHandleCyclePRAutopilot(t *testing.T) {
	_, prPoll, conn := setupHandlerWithPR(t)
	defer conn.Close()

	prPoll.Add("owner", "repo", 1) // Starts at auto.

	// auto -> yolo
	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "owner/repo#1",
	})
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok=true")
	}
	if resp.AutopilotMode != "yolo" {
		t.Errorf("mode = %q, want yolo", resp.AutopilotMode)
	}

	// yolo -> off
	resp = sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "owner/repo#1",
	})
	if resp.AutopilotMode != "off" {
		t.Errorf("mode = %q, want off", resp.AutopilotMode)
	}

	// off -> auto
	resp = sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "owner/repo#1",
	})
	if resp.AutopilotMode != "auto" {
		t.Errorf("mode = %q, want auto", resp.AutopilotMode)
	}
}

func TestHandleCyclePRAutopilot_NonexistentPR(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "owner/repo#999",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for nonexistent PR")
	}
}

func TestHandleCyclePRAutopilot_InvalidKey(t *testing.T) {
	_, _, conn := setupHandlerWithPR(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "badkey",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false for invalid key")
	}
}

func TestHandleCyclePRAutopilot_NoPRPoller(t *testing.T) {
	_, conn := setupHandler(t)
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{
		Action: "cycle_pr_autopilot",
		PRKey:  "owner/repo#1",
	})
	if resp.OK == nil || *resp.OK {
		t.Error("expected ok=false when no PR poller")
	}
}

// --- list includes PRs ---

func TestHandleList_IncludesPRs(t *testing.T) {
	_, prPoll, conn := setupHandlerWithPR(t)
	defer conn.Close()

	prPoll.Add("octocat", "hello-world", 42)
	prPoll.Add("owner", "repo", 7)

	resp := sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if len(resp.PRs) != 2 {
		t.Errorf("list: got %d PRs, want 2", len(resp.PRs))
	}
}

func TestHandleList_NilPRPoller(t *testing.T) {
	_, conn := setupHandler(t) // No PR poller.
	defer conn.Close()

	resp := sendAndReceive(t, conn, ctlRequest{Action: "list"})
	if len(resp.PRs) != 0 {
		t.Errorf("nil poller: got %d PRs, want 0", len(resp.PRs))
	}
}

// --- subscribe includes PRs ---

func TestHandleSubscribe_IncludesPRs(t *testing.T) {
	st := newTestState(t)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	prPoll := pr.NewPoller(storePath, func() {
		st.NotifySubscribers()
	})
	h := NewHandler(st, prPoll)

	server, client := net.Pipe()
	go h.Handle(server)
	client.SetDeadline(time.Now().Add(5 * time.Second))
	defer client.Close()

	// Add a PR before subscribing.
	prPoll.Add("owner", "repo", 1)

	// Send subscribe.
	data, _ := json.Marshal(ctlRequest{Action: "subscribe"})
	data = append(data, '\n')
	client.Write(data)

	// Read the initial state snapshot.
	scanner := bufio.NewScanner(client)
	if !scanner.Scan() {
		t.Fatal("no initial snapshot")
	}
	var resp ctlResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Event != "state_updated" {
		t.Errorf("event = %q, want state_updated", resp.Event)
	}
	if len(resp.PRs) != 1 {
		t.Errorf("subscribe snapshot: got %d PRs, want 1", len(resp.PRs))
	}
}
