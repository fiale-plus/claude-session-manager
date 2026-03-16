package hookserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/pr"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

// --- Handle via Unix socket ---

func TestHandleViaUnixSocket(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)

	// Create a temporary Unix socket.
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Start a goroutine to accept and handle one connection.
	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.Handle(conn)
		close(done)
	}()

	// Connect and send a SessionStart request.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	req := hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/home/test",
		Slug:          "test-session",
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)

	// Read the response.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read error: %v", err)
	}
	conn.Close()

	<-done

	// Should have received a valid JSON response.
	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v (got: %s)", err, string(buf[:n]))
	}

	// SessionStart should have been processed.
	sessions := st.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].SessionID != "s1" {
		t.Errorf("sessionID = %q, want s1", sessions[0].SessionID)
	}
}

func TestHandleInvalidJSON(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		h.Handle(conn)
		done <- nil
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	// Send invalid JSON.
	conn.Write([]byte("not valid json\n"))

	// Read response.
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	conn.Close()
	<-done

	// Should still get a response (empty hookResponse).
	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("expected valid JSON even for invalid request: %v", err)
	}
}

func TestHandleEmptyRead(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(done)
			return
		}
		h.Handle(conn)
		close(done)
	}()

	// Connect and immediately close → scanner.Scan() returns false.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Handle should return when connection closes")
	}
}

func TestHandleUnknownEvent(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(done)
			return
		}
		h.Handle(conn)
		close(done)
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	req := hookRequest{HookEventName: "UnknownEvent", SessionID: "s1"}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))

	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	conn.Close()
	<-done

	// Should respond with empty response.
	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("response: %v", err)
	}
	if resp.HookSpecificOutput != nil {
		t.Error("unknown event should return empty hookSpecificOutput")
	}
}

// --- SetPRPoller ---

func TestSetPRPoller(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	h.SetPRPoller(p)

	if h.prPoll != p {
		t.Error("SetPRPoller should set the poller")
	}
}

// --- PostToolUse ---

func TestPostToolUseNoPRPoller(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	// No PR poller set — should not panic.
	h.handlePostToolUse(hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":"https://github.com/owner/repo/pull/42"}`),
	})
}

func TestPostToolUseEmptyOutput(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	h.SetPRPoller(p)

	// Empty tool output.
	h.handlePostToolUse(hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":""}`),
	})

	// Should not add any PR.
	if len(p.GetAll()) != 0 {
		t.Error("empty output should not add any PR")
	}
}

func TestPostToolUseNoPRURL(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	h.SetPRPoller(p)

	// Tool output without PR URL.
	h.handlePostToolUse(hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":"File created at /tmp/test.go"}`),
	})

	if len(p.GetAll()) != 0 {
		t.Error("output without PR URL should not add any PR")
	}
}

func TestPostToolUseWithPRURL(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	h.SetPRPoller(p)

	h.handlePostToolUse(hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":"Created PR: https://github.com/octocat/hello/pull/42"}`),
	})

	// Give the goroutine a moment to start Poll (it will fail but we don't care).
	time.Sleep(50 * time.Millisecond)

	all := p.GetAll()
	if len(all) != 1 {
		t.Fatalf("got %d PRs, want 1", len(all))
	}
	if all[0].Owner != "octocat" {
		t.Errorf("owner = %q, want octocat", all[0].Owner)
	}
	if all[0].Repo != "hello" {
		t.Errorf("repo = %q, want hello", all[0].Repo)
	}
	if all[0].Number != 42 {
		t.Errorf("number = %d, want 42", all[0].Number)
	}
}

// --- writeJSON ---

func TestWriteJSON(t *testing.T) {
	// Use a pipe as net.Conn.
	server, client := net.Pipe()
	defer server.Close()

	go writeJSON(server, hookResponse{
		HookSpecificOutput: &hookOutput{
			HookEventName: "PreToolUse",
			Decision:      "allow",
		},
	})

	buf := make([]byte, 4096)
	n, _ := client.Read(buf)
	client.Close()

	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.Decision != "allow" {
		t.Errorf("decision = %q, want allow", resp.HookSpecificOutput.Decision)
	}
}

func TestWriteJSONEmptyResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	go writeJSON(server, hookResponse{})

	buf := make([]byte, 4096)
	n, _ := client.Read(buf)
	client.Close()

	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// --- HTTPServer.handleHook ---

func TestHTTPServerHandleHook_AllEvents(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	srv.SetPRPoller(p)

	// Use httptest to test the handler directly.
	ts := httptest.NewServer(srv.server.Handler)
	defer ts.Close()

	// SessionStart
	postTo(t, ts.URL+"/hooks", hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})

	sessions := st.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("after SessionStart: got %d sessions, want 1", len(sessions))
	}

	// PostToolUse with PR URL
	postTo(t, ts.URL+"/hooks", hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":"PR: https://github.com/test/repo/pull/99"}`),
	})

	time.Sleep(50 * time.Millisecond)
	if len(p.GetAll()) != 1 {
		t.Errorf("PostToolUse should add PR, got %d", len(p.GetAll()))
	}

	// SessionEnd
	postTo(t, ts.URL+"/hooks", hookRequest{
		HookEventName: "SessionEnd",
		SessionID:     "s1",
	})
	sessions = st.GetSessions()
	if len(sessions) != 0 {
		t.Errorf("after SessionEnd: got %d sessions, want 0", len(sessions))
	}
}

func TestHTTPServerHandleHook_InvalidJSON(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)
	ts := httptest.NewServer(srv.server.Handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/hooks", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPServerHandleHook_MethodNotAllowed(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)
	ts := httptest.NewServer(srv.server.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/hooks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHTTPServerHandleHook_UnknownEvent(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)
	ts := httptest.NewServer(srv.server.Handler)
	defer ts.Close()

	body, _ := json.Marshal(hookRequest{HookEventName: "SomeUnknownEvent"})
	resp, err := http.Post(ts.URL+"/hooks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// --- Server (Unix socket server) ---

func TestServerNewAndClose(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	st := newTestState()

	srv, err := New(sockPath, st)
	if err != nil {
		t.Fatal(err)
	}

	// Verify socket file exists.
	if _, err := os.Stat(sockPath); err != nil {
		t.Errorf("socket file should exist: %v", err)
	}

	// Close should succeed.
	if err := srv.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}
}

func TestServerServeAndClose(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	st := newTestState()

	srv, err := New(sockPath, st)
	if err != nil {
		t.Fatal(err)
	}

	// Start serving.
	done := make(chan struct{})
	go func() {
		srv.Serve()
		close(done)
	}()

	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Connect and send a SessionStart.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	req := hookRequest{HookEventName: "SessionStart", SessionID: "s1", CWD: "/test"}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))
	buf := make([]byte, 4096)
	conn.Read(buf)
	conn.Close()

	// Wait for processing.
	time.Sleep(50 * time.Millisecond)

	sessions := st.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	// Close server.
	srv.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestServerSetPRPoller(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	st := newTestState()

	srv, err := New(sockPath, st)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	srv.SetPRPoller(p)

	if srv.handler.prPoll != p {
		t.Error("SetPRPoller should propagate to handler")
	}
}

func TestServerRemovesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	// Create a stale socket file.
	os.WriteFile(sockPath, []byte("stale"), 0o644)

	st := newTestState()
	srv, err := New(sockPath, st)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	// Should succeed even though a file existed.
}

// --- HTTPServer.Close ---

func TestHTTPServerClose(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)

	ts := httptest.NewServer(srv.server.Handler)
	ts.Close()

	// Also test Close on the HTTPServer itself.
	err := srv.Close()
	if err != nil {
		// Close on an unstarted server may fail, that's ok.
		t.Logf("Close error (expected): %v", err)
	}
}

// --- PreToolUse with passthrough decision ---

func TestPreToolUseDenyViaSocket(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	st.RegisterSession("s1", "/path", "default")

	done := make(chan hookResponse, 1)
	go func() {
		resp := h.handlePreToolUse(hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "rm -rf /"},
		})
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)
	st.ResolvePending("s1", model.DecisionDeny)

	select {
	case resp := <-done:
		if resp.HookSpecificOutput == nil {
			t.Fatal("expected hookSpecificOutput")
		}
		if resp.HookSpecificOutput.Decision != "deny" {
			t.Errorf("decision = %q, want deny", resp.HookSpecificOutput.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- Passthrough decision ---

func TestPreToolUsePassthroughDecision(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	st.RegisterSession("s1", "/path", "default")

	done := make(chan hookResponse, 1)
	go func() {
		resp := h.handlePreToolUse(hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "ls"},
		})
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)
	st.ResolvePending("s1", model.DecisionPassthrough)

	select {
	case resp := <-done:
		// Passthrough returns empty response.
		if resp.HookSpecificOutput != nil {
			t.Errorf("passthrough should return empty hookSpecificOutput, got %+v", resp.HookSpecificOutput)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- YOLO grace period allow ---

func TestPreToolUseYoloGraceAllowed(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	st.RegisterSession("s1", "/path", "default")
	st.CycleAutopilot("s1") // off → on
	st.CycleAutopilot("s1") // on → yolo

	done := make(chan hookResponse, 1)
	go func() {
		resp := h.handlePreToolUse(hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "git push --force"},
		})
		done <- resp
	}()

	// Let the grace period goroutine start, then explicitly allow.
	time.Sleep(100 * time.Millisecond)
	st.ResolvePending("s1", model.DecisionAllow)

	select {
	case resp := <-done:
		if resp.HookSpecificOutput == nil {
			t.Fatal("expected hookSpecificOutput")
		}
		if resp.HookSpecificOutput.Decision != "allow" {
			t.Errorf("decision = %q, want allow", resp.HookSpecificOutput.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- YOLO grace period passthrough ---

func TestPreToolUseYoloGracePassthrough(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	st.RegisterSession("s1", "/path", "default")
	st.CycleAutopilot("s1") // off → on
	st.CycleAutopilot("s1") // on → yolo

	done := make(chan hookResponse, 1)
	go func() {
		resp := h.handlePreToolUse(hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "git push --force"},
		})
		done <- resp
	}()

	time.Sleep(100 * time.Millisecond)
	st.ResolvePending("s1", model.DecisionPassthrough)

	select {
	case resp := <-done:
		// Passthrough returns empty response.
		if resp.HookSpecificOutput != nil {
			t.Errorf("passthrough should return empty hookSpecificOutput")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// --- Handle with PreToolUse through socket ---

func TestHandlePreToolUseViaSocket(t *testing.T) {
	st := newTestState()
	st.RegisterSession("s1", "/path", "default")
	st.CycleAutopilot("s1") // off → on

	h := NewHandler(st)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.Handle(conn)
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	// Safe tool with autopilot on → auto-approve.
	req := hookRequest{
		HookEventName: "PreToolUse",
		SessionID:     "s1",
		ToolName:      "Read",
		ToolInput:     map[string]any{"file_path": "/a.py"},
	}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read error: %v", err)
	}
	conn.Close()

	var resp hookResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.Decision != "allow" {
		t.Errorf("decision = %q, want allow", resp.HookSpecificOutput.Decision)
	}
}

// --- Handle with PostToolUse through socket ---

func TestHandlePostToolUseViaSocket(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := pr.NewPoller(storePath, nil)
	h.SetPRPoller(p)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.Handle(conn)
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	req := hookRequest{
		HookEventName: "PostToolUse",
		SessionID:     "s1",
		ToolResponse: json.RawMessage(`{"stdout":"PR created: https://github.com/test/repo/pull/1"}`),
	}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))

	buf := make([]byte, 4096)
	conn.Read(buf)
	conn.Close()

	time.Sleep(100 * time.Millisecond)

	all := p.GetAll()
	if len(all) != 1 {
		t.Fatalf("got %d PRs, want 1", len(all))
	}
}

// --- Handle with SessionEnd through socket ---

func TestHandleSessionEndViaSocket(t *testing.T) {
	st := newTestState()
	st.RegisterSession("s1", "/path", "default")
	h := NewHandler(st)

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.Handle(conn)
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}

	req := hookRequest{HookEventName: "SessionEnd", SessionID: "s1"}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))
	buf := make([]byte, 4096)
	conn.Read(buf)
	conn.Close()

	time.Sleep(50 * time.Millisecond)
	sessions := st.GetSessions()
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

// --- prURLRe ---

func TestPRURLRegex(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Created https://github.com/octocat/hello/pull/42", "https://github.com/octocat/hello/pull/42"},
		{"https://github.com/a/b/pull/1", "https://github.com/a/b/pull/1"},
		{"No PR here", ""},
		{"https://github.com/a/b/issues/1", ""},
		{"multiple https://github.com/a/b/pull/1 and https://github.com/c/d/pull/2", "https://github.com/a/b/pull/1"},
	}
	for _, tt := range tests {
		got := prURLRe.FindString(tt.input)
		if got != tt.want {
			t.Errorf("prURLRe.FindString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- helper ---

func postTo(t *testing.T, url string, req hookRequest) hookResponse {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	var hookResp hookResponse
	json.NewDecoder(resp.Body).Decode(&hookResp)
	return hookResp
}

// newTestStateForHandler creates a state.Manager with isolated disk persistence.
func newTestStateIsolated() *state.Manager {
	dir, _ := os.MkdirTemp("", "csm-hooktest-*")
	return state.NewWithDir(dir)
}
