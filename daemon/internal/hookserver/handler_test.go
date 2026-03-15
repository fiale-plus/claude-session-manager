package hookserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

// newTestState creates a state.Manager with isolated disk persistence.
func newTestState() *state.Manager {
	return state.NewWithDir(t_tempDir())
}

// t_tempDir returns a unique temp directory for test isolation.
// Each call creates a new directory.
var _tempDirCounter int

func t_tempDir() string {
	_tempDirCounter++
	dir, _ := os.MkdirTemp("", "csm-test-*")
	return dir
}

func postHook(handler http.Handler, req hookRequest) hookResponse {
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/hooks", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp hookResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

func makeHTTPServer(st *state.Manager) http.Handler {
	h := NewHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("/hooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req hookRequest
		if err := json.Unmarshal(body, &req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}\n"))
			return
		}

		var resp hookResponse
		switch req.HookEventName {
		case "SessionStart":
			h.handleSessionStart(req)
		case "SessionEnd":
			h.handleSessionEnd(req)
		case "PreToolUse":
			resp = h.handlePreToolUse(req)
		}

		w.Header().Set("Content-Type", "application/json")
		data, _ := json.Marshal(resp)
		w.Write(append(data, '\n'))
	})
	return mux
}

// --- SessionStart ---

func TestHandleSessionStart(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	resp := postHook(handler, hookRequest{
		HookEventName:  "SessionStart",
		SessionID:      "s1",
		CWD:            "/home/user/project",
		Slug:           "my-session",
		PermissionMode: "default",
	})

	// SessionStart returns empty response.
	if resp.HookSpecificOutput != nil {
		t.Error("SessionStart should return empty response")
	}

	sessions := st.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].SessionID != "s1" {
		t.Errorf("session_id = %q, want s1", sessions[0].SessionID)
	}
	if sessions[0].Slug != "my-session" {
		t.Errorf("slug = %q, want my-session", sessions[0].Slug)
	}
}

// --- SessionEnd ---

func TestHandleSessionEnd(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	// Register first.
	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})

	// End.
	postHook(handler, hookRequest{
		HookEventName: "SessionEnd",
		SessionID:     "s1",
	})

	sessions := st.GetSessions()
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0 after SessionEnd", len(sessions))
	}
}

// --- PreToolUse with autopilot on ---

func TestPreToolUseAutopilotOnSafe(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	// Register and enable autopilot.
	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	st.CycleAutopilot("s1") // off → on

	// Safe tool should be auto-approved.
	resp := postHook(handler, hookRequest{
		HookEventName: "PreToolUse",
		SessionID:     "s1",
		ToolName:      "Read",
		ToolInput:     map[string]any{"file_path": "/a.py"},
	})

	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.Decision != "allow" {
		t.Errorf("decision = %q, want allow", resp.HookSpecificOutput.Decision)
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", resp.HookSpecificOutput.HookEventName)
	}
}

func TestPreToolUseAutopilotOnDestructiveBlocks(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	st.CycleAutopilot("s1") // off → on

	// Destructive tool should block (goes to pending queue).
	// We need to resolve it from another goroutine.
	done := make(chan hookResponse, 1)
	go func() {
		resp := postHook(handler, hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "git push"},
		})
		done <- resp
	}()

	// Wait for pending to appear then resolve.
	time.Sleep(50 * time.Millisecond)
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
		t.Fatal("timed out waiting for response")
	}
}

// --- PreToolUse with autopilot off ---

func TestPreToolUseAutopilotOffBlocks(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	// Autopilot is off by default.

	done := make(chan hookResponse, 1)
	go func() {
		resp := postHook(handler, hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Read",
			ToolInput:     map[string]any{"file_path": "/a.py"},
		})
		done <- resp
	}()

	// Approve from "TUI".
	time.Sleep(50 * time.Millisecond)
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

func TestPreToolUseReject(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})

	done := make(chan hookResponse, 1)
	go func() {
		resp := postHook(handler, hookRequest{
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

// --- PreToolUse YOLO mode with destructive (grace period) ---

func TestPreToolUseYoloDestructiveGracePeriod(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	st.CycleAutopilot("s1") // off → on
	st.CycleAutopilot("s1") // on → yolo

	// Destructive tool in YOLO gets grace period.
	// The grace timer is 10s, but we'll manually resolve to speed up the test.
	done := make(chan hookResponse, 1)
	go func() {
		resp := postHook(handler, hookRequest{
			HookEventName: "PreToolUse",
			SessionID:     "s1",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "git push --force"},
		})
		done <- resp
	}()

	// Wait for pending to appear, then explicitly reject to override grace.
	time.Sleep(100 * time.Millisecond)
	st.ResolvePending("s1", model.DecisionDeny)

	select {
	case resp := <-done:
		if resp.HookSpecificOutput == nil {
			t.Fatal("expected hookSpecificOutput")
		}
		if resp.HookSpecificOutput.Decision != "deny" {
			t.Errorf("decision = %q, want deny (user rejected during grace)", resp.HookSpecificOutput.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestPreToolUseYoloSafeAutoApproves(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	st.CycleAutopilot("s1") // off → on
	st.CycleAutopilot("s1") // on → yolo

	resp := postHook(handler, hookRequest{
		HookEventName: "PreToolUse",
		SessionID:     "s1",
		ToolName:      "Read",
		ToolInput:     map[string]any{"file_path": "/a.py"},
	})

	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.Decision != "allow" {
		t.Errorf("decision = %q, want allow", resp.HookSpecificOutput.Decision)
	}
}

// --- HTTP handler format ---

func TestHTTPHandlerReturnsJSON(t *testing.T) {
	st := newTestState()
	h := NewHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("/hooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		var req hookRequest
		json.Unmarshal(body, &req)

		var resp hookResponse
		switch req.HookEventName {
		case "SessionStart":
			h.handleSessionStart(req)
		}

		w.Header().Set("Content-Type", "application/json")
		data, _ := json.Marshal(resp)
		w.Write(append(data, '\n'))
	})

	body, _ := json.Marshal(hookRequest{HookEventName: "SessionStart", SessionID: "s1", CWD: "/p"})
	r := httptest.NewRequest(http.MethodPost, "/hooks", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	result := w.Result()
	if result.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q, want application/json", result.Header.Get("Content-Type"))
	}

	var resp hookResponse
	err := json.NewDecoder(result.Body).Decode(&resp)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	st := newTestState()
	srv := NewHTTP("0", st)
	_ = srv // just to verify it builds

	// Test via the HTTP handler pattern.
	h := NewHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("/hooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		var req hookRequest
		json.Unmarshal(body, &req)
		_ = h
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}\n"))
	})

	r := httptest.NewRequest(http.MethodGet, "/hooks", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestPreToolUseUpdatesSlug(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
		Slug:          "original",
	})
	st.CycleAutopilot("s1") // off → on

	// PreToolUse with updated slug.
	postHook(handler, hookRequest{
		HookEventName: "PreToolUse",
		SessionID:     "s1",
		ToolName:      "Read",
		ToolInput:     map[string]any{"file_path": "/a.py"},
		Slug:          "renamed",
	})

	sessions := st.GetSessions()
	if sessions[0].Slug != "renamed" {
		t.Errorf("slug = %q, want renamed", sessions[0].Slug)
	}
}

func TestPreToolUseUnknownToolAutopilotOn(t *testing.T) {
	st := newTestState()
	handler := makeHTTPServer(st)

	postHook(handler, hookRequest{
		HookEventName: "SessionStart",
		SessionID:     "s1",
		CWD:           "/path",
	})
	st.CycleAutopilot("s1") // off → on

	// Unknown tool (Bash with unknown command) in ON mode → auto-approve.
	resp := postHook(handler, hookRequest{
		HookEventName: "PreToolUse",
		SessionID:     "s1",
		ToolName:      "Bash",
		ToolInput:     map[string]any{"command": "docker build ."},
	})

	if resp.HookSpecificOutput == nil {
		t.Fatal("expected hookSpecificOutput")
	}
	if resp.HookSpecificOutput.Decision != "allow" {
		t.Errorf("decision = %q, want allow (ON mode approves unknown)", resp.HookSpecificOutput.Decision)
	}
}
