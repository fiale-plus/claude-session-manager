package ctlserver

import (
	"bufio"
	"encoding/json"
	"log"
	"net"

	"github.com/pchaganti/claude-session-manager/daemon/internal/ghostty"
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
		case "focus":
			h.handleFocus(conn, req.SessionID)
		case "approve":
			h.handleApprove(conn, req.SessionID)
		case "reject":
			h.handleReject(conn, req.SessionID)
		case "approve_all":
			h.handleApproveAll(conn)
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

func (h *Handler) handleFocus(conn net.Conn, sid string) {
	sessions := h.state.GetSessions()
	ok := false
	for _, s := range sessions {
		if s.SessionID == sid && s.GhosttyTab != "" {
			ok = ghostty.SwitchToTab(s.GhosttyTab)
			break
		}
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleApprove(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionAllow)
	if !ok {
		// Fallback: write "y\n" directly to the session's TTY.
		sessions := h.state.GetSessions()
		for _, s := range sessions {
			if s.SessionID == sid && s.TTY != "" {
				ok = ghostty.SendApprovalToTTY(s.TTY)
				if ok {
					log.Printf("ctl: approved %s via TTY %s", sid, s.TTY)
				}
				break
			}
		}
	}
	if !ok {
		log.Printf("ctl: approve failed for %s", sid)
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleReject(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionDeny)
	if !ok {
		sessions := h.state.GetSessions()
		for _, s := range sessions {
			if s.SessionID == sid && s.TTY != "" {
				ok = ghostty.SendRejectionToTTY(s.TTY)
				if ok {
					log.Printf("ctl: rejected %s via TTY %s", sid, s.TTY)
				}
				break
			}
		}
	}
	if !ok {
		log.Printf("ctl: reject failed for %s", sid)
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleApproveAll(conn net.Conn) {
	count := h.state.ApproveAllPending()
	ok := count > 0
	log.Printf("ctl: approve_all — approved %d sessions", count)
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
