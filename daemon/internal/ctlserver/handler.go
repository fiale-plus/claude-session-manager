package ctlserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/pchaganti/claude-session-manager/daemon/internal/ghostty"
	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
	"github.com/pchaganti/claude-session-manager/daemon/internal/pr"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

type ctlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`     // for add_pr
	PRKey     string `json:"pr_key,omitempty"`     // "owner/repo#N" for remove_pr
}

type ctlResponse struct {
	OK            *bool           `json:"ok,omitempty"`
	Sessions      []model.Session `json:"sessions,omitempty"`
	PRs           []pr.TrackedPR  `json:"prs,omitempty"`
	Event         string          `json:"event,omitempty"`
	AutopilotMode string          `json:"autopilot_mode,omitempty"`
}

type Handler struct {
	state  *state.Manager
	prPoll *pr.Poller
}

func NewHandler(st *state.Manager, prPoll *pr.Poller) *Handler {
	return &Handler{state: st, prPoll: prPoll}
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
		case "add_pr":
			h.handleAddPR(conn, req.PRURL)
		case "remove_pr":
			h.handleRemovePR(conn, req.PRKey)
		}
	}
}

func (h *Handler) handleList(conn net.Conn) {
	var prs []pr.TrackedPR
	if h.prPoll != nil {
		prs = h.prPoll.GetAll()
	}
	writeJSON(conn, ctlResponse{Sessions: h.state.GetSessions(), PRs: prs})
}

func (h *Handler) handleSubscribe(conn net.Conn) {
	ch := h.state.Subscribe()
	defer h.state.Unsubscribe(ch)

	resp := h.stateSnapshot()
	writeJSON(conn, resp)
	for range ch {
		resp := h.stateSnapshot()
		writeJSON(conn, resp)
	}
}

func (h *Handler) stateSnapshot() ctlResponse {
	resp := ctlResponse{
		Event:    "state_updated",
		Sessions: h.state.GetSessions(),
	}
	if h.prPoll != nil {
		resp.PRs = h.prPoll.GetAll()
	}
	return resp
}

func (h *Handler) handleToggleAutopilot(conn net.Conn, sid string) {
	mode, ok := h.state.CycleAutopilot(sid)
	writeJSON(conn, ctlResponse{OK: &ok, AutopilotMode: mode})
}

func (h *Handler) handleFocus(conn net.Conn, sid string) {
	ok := false
	for _, s := range h.state.GetSessions() {
		if s.SessionID == sid && s.GhosttyTabIndex > 0 {
			ok = ghostty.SwitchToTabByIndex(s.GhosttyTabIndex)
			if ok {
				log.Printf("ctl: focused %s → tab index %d", sid, s.GhosttyTabIndex)
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

func (h *Handler) handleAddPR(conn net.Conn, url string) {
	if h.prPoll == nil {
		f := false
		writeJSON(conn, ctlResponse{OK: &f})
		return
	}
	tracked, err := h.prPoll.AddFromURL(url)
	if err != nil {
		log.Printf("ctl: add_pr failed: %v", err)
		f := false
		writeJSON(conn, ctlResponse{OK: &f})
		return
	}
	log.Printf("ctl: added PR %s/%s#%d", tracked.Owner, tracked.Repo, tracked.Number)
	ok := true
	writeJSON(conn, ctlResponse{OK: &ok})
	// Trigger immediate poll for the new PR.
	go h.prPoll.Poll()
}

func (h *Handler) handleRemovePR(conn net.Conn, key string) {
	if h.prPoll == nil {
		f := false
		writeJSON(conn, ctlResponse{OK: &f})
		return
	}
	// Parse "owner/repo#N"
	parts := strings.SplitN(key, "#", 2)
	if len(parts) != 2 {
		f := false
		writeJSON(conn, ctlResponse{OK: &f})
		return
	}
	ownerRepo := strings.SplitN(parts[0], "/", 2)
	if len(ownerRepo) != 2 {
		f := false
		writeJSON(conn, ctlResponse{OK: &f})
		return
	}
	var number int
	fmt.Sscanf(parts[1], "%d", &number)
	h.prPoll.Remove(ownerRepo[0], ownerRepo[1], number)
	ok := true
	writeJSON(conn, ctlResponse{OK: &ok})
}

func writeJSON(conn net.Conn, v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	_, _ = conn.Write(data)
}
