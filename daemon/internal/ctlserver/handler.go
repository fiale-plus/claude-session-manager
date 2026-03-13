package ctlserver

import (
	"bufio"
	"encoding/json"
	"log"
	"net"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

// ctlRequest is a JSON message from a TUI client.
type ctlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
}

// ctlResponse is a JSON response to a TUI client.
type ctlResponse struct {
	OK       *bool           `json:"ok,omitempty"`
	Sessions []model.Session `json:"sessions,omitempty"`
	Event    string          `json:"event,omitempty"`
	// For toggle_autopilot:
	Autopilot *bool `json:"autopilot,omitempty"`
}

// Handler manages a single TUI client connection.
type Handler struct {
	state *state.Manager
}

// NewHandler creates a handler for a TUI client.
func NewHandler(st *state.Manager) *Handler {
	return &Handler{state: st}
}

// Handle processes requests from a persistent TUI connection.
func (h *Handler) Handle(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		var req ctlRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			log.Printf("ctl: invalid JSON: %v", err)
			continue
		}

		switch req.Action {
		case "list":
			h.handleList(conn)
		case "subscribe":
			h.handleSubscribe(conn)
			return // subscribe takes over the connection
		case "toggle_autopilot":
			h.handleToggleAutopilot(conn, req.SessionID)
		case "approve":
			h.handleApprove(conn, req.SessionID)
		case "reject":
			h.handleReject(conn, req.SessionID)
		default:
			log.Printf("ctl: unknown action: %s", req.Action)
		}
	}
}

func (h *Handler) handleList(conn net.Conn) {
	sessions := h.state.GetSessions()
	writeJSON(conn, ctlResponse{Sessions: sessions})
}

func (h *Handler) handleSubscribe(conn net.Conn) {
	ch := h.state.Subscribe()
	defer h.state.Unsubscribe(ch)

	// Send initial state.
	sessions := h.state.GetSessions()
	writeJSON(conn, ctlResponse{
		Event:    "sessions_updated",
		Sessions: sessions,
	})

	// Stream updates.
	for range ch {
		sessions := h.state.GetSessions()
		writeJSON(conn, ctlResponse{
			Event:    "sessions_updated",
			Sessions: sessions,
		})
	}
}

func (h *Handler) handleToggleAutopilot(conn net.Conn, sid string) {
	newState, ok := h.state.ToggleAutopilot(sid)
	bOK := ok
	writeJSON(conn, ctlResponse{OK: &bOK, Autopilot: &newState})
}

func (h *Handler) handleApprove(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionAllow)
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleReject(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionDeny)
	writeJSON(conn, ctlResponse{OK: &ok})
}

func writeJSON(conn net.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}
