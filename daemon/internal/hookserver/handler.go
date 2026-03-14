package hookserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/classifier"
	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

// hookRequest is the JSON sent by CC hooks via csm-hook.sh.
type hookRequest struct {
	HookEventName  string         `json:"hook_event_name"`
	SessionID      string         `json:"session_id"`
	CWD            string         `json:"cwd"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	PermissionMode string         `json:"permission_mode"`
}

// hookResponse is the JSON returned to CC hooks.
type hookResponse struct {
	HookSpecificOutput *hookOutput `json:"hookSpecificOutput,omitempty"`
}

type hookOutput struct {
	HookEventName string `json:"hookEventName,omitempty"`
	Decision      string `json:"permissionDecision,omitempty"`
}

// Handler processes individual hook connections.
type Handler struct {
	state *state.Manager
}

// NewHandler creates a new hook handler.
func NewHandler(st *state.Manager) *Handler {
	return &Handler{state: st}
}

// Handle reads one request from conn, processes it, writes a response, and closes.
func (h *Handler) Handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return
	}

	raw := bytes.TrimSpace(scanner.Bytes())

	var req hookRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		log.Printf("hook: invalid JSON: %v (raw: %.200s)", err, string(raw))
		writeJSON(conn, hookResponse{})
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
	default:
		log.Printf("hook: unknown event: %s", req.HookEventName)
	}

	writeJSON(conn, resp)
}

func (h *Handler) handleSessionStart(req hookRequest) {
	log.Printf("hook: SessionStart session=%s cwd=%s", req.SessionID, req.CWD)
	h.state.RegisterSession(req.SessionID, req.CWD, req.PermissionMode)
}

func (h *Handler) handleSessionEnd(req hookRequest) {
	log.Printf("hook: SessionEnd session=%s", req.SessionID)
	h.state.UnregisterSession(req.SessionID)
}

func (h *Handler) handlePreToolUse(req hookRequest) hookResponse {
	log.Printf("hook: PreToolUse session=%s tool=%s", req.SessionID, req.ToolName)

	tool := model.PendingTool{
		ToolName:  req.ToolName,
		ToolInput: req.ToolInput,
	}

	// Check if autopilot should auto-approve — tells CC to skip permission prompt.
	safety := classifyQuick(tool)
	if h.state.ShouldAutoApprove(req.SessionID, safety) {
		log.Printf("hook: auto-approve %s (safety=%s)", req.ToolName, safety)
		return hookResponse{
			HookSpecificOutput: &hookOutput{HookEventName: "PreToolUse", Decision: "allow"},
		}
	}

	// If destructive or autopilot is off, add to pending queue and wait for user.
	decisionCh := h.state.AddPending(req.SessionID, tool)

	// Wait for user decision (from TUI or timeout).
	select {
	case decision := <-decisionCh:
		switch decision {
		case model.DecisionAllow:
			return hookResponse{
				HookSpecificOutput: &hookOutput{HookEventName: "PreToolUse", Decision: "allow"},
			}
		case model.DecisionDeny:
			return hookResponse{
				HookSpecificOutput: &hookOutput{HookEventName: "PreToolUse", Decision: "deny"},
			}
		default:
			// Passthrough — let CC handle it normally.
			return hookResponse{}
		}
	case <-time.After(60 * time.Second):
		// Timeout — passthrough to CC's default permission logic.
		log.Printf("hook: timeout waiting for decision on %s (60s)", req.ToolName)
		return hookResponse{}
	}
}

func classifyQuick(tool model.PendingTool) model.ToolSafety {
	return classifier.ClassifyTool(tool.ToolName, tool.ToolInput)
}

func writeJSON(conn net.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}
