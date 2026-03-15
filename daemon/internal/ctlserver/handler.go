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

type ctlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
}

type ctlResponse struct {
	OK        *bool           `json:"ok,omitempty"`
	Sessions  []model.Session `json:"sessions,omitempty"`
	Event     string          `json:"event,omitempty"`
	Autopilot *bool           `json:"autopilot,omitempty"`
}

type Handler struct {
	state *state.Manager
}

func NewHandler(st *state.Manager) *Handler {
	return &Handler{state: st}
}

func (h *Handler) Handle(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		var req ctlRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		switch req.Action {
		case "list":
			h.handleList(conn)
		case "subscribe":
			h.handleSubscribe(conn)
			return
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
		}
	}
}

func (h *Handler) handleList(conn net.Conn) {
	writeJSON(conn, ctlResponse{Sessions: h.state.GetSessions()})
}

func (h *Handler) handleSubscribe(conn net.Conn) {
	ch := h.state.Subscribe()
	defer h.state.Unsubscribe(ch)

	writeJSON(conn, ctlResponse{Event: "sessions_updated", Sessions: h.state.GetSessions()})
	for range ch {
		writeJSON(conn, ctlResponse{Event: "sessions_updated", Sessions: h.state.GetSessions()})
	}
}

func (h *Handler) handleToggleAutopilot(conn net.Conn, sid string) {
	newState, ok := h.state.ToggleAutopilot(sid)
	writeJSON(conn, ctlResponse{OK: &ok, Autopilot: &newState})
}

func (h *Handler) handleFocus(conn net.Conn, sid string) {
	ok := false
	for _, s := range h.state.GetSessions() {
		if s.SessionID == sid && s.GhosttyTab != "" {
			ok = ghostty.SwitchToTab(s.GhosttyTab)
			if ok {
				log.Printf("ctl: focused %s → tab %q", sid, s.GhosttyTab)
			}
			break
		}
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleApprove(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionAllow)
	if ok {
		log.Printf("ctl: approved %s", sid)
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleReject(conn net.Conn, sid string) {
	ok := h.state.ResolvePending(sid, model.DecisionDeny)
	if ok {
		log.Printf("ctl: rejected %s", sid)
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func (h *Handler) handleApproveAll(conn net.Conn) {
	count := h.state.ApproveAllPending()
	ok := count > 0
	if ok {
		log.Printf("ctl: approve_all — %d sessions", count)
	}
	writeJSON(conn, ctlResponse{OK: &ok})
}

func writeJSON(conn net.Conn, v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	_, _ = conn.Write(data)
}
