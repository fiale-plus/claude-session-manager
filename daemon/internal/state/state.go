// Package state manages the daemon's central state: session registry,
// autopilot toggles, and pending approval queue.
package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/classifier"
	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// stateOrder defines sort priority: lower number = higher priority.
var stateOrder = map[model.SessionState]int{
	model.StateRunning: 0,
	model.StateWaiting: 1,
	model.StateIdle:    2,
	model.StateDead:    3,
}

// Manager is the central state store for the daemon.
type Manager struct {
	mu sync.RWMutex

	// sessions maps session_id → Session.
	sessions map[string]*model.Session

	// autopilot maps session_id → enabled. Persisted to disk.
	autopilot map[string]bool

	// pending maps session_id → PendingApproval (at most one per session).
	pending map[string]*model.PendingApproval

	// cooldowns maps session_id → last approval time (prevents double-approve).
	cooldowns map[string]time.Time

	// subscribers receive notifications on state changes.
	subscribers []chan struct{}
	subMu       sync.Mutex

	autopilotPath string
}

// New creates a new state Manager, loading persisted autopilot state.
func New() *Manager {
	m := &Manager{
		sessions:  make(map[string]*model.Session),
		autopilot: make(map[string]bool),
		pending:   make(map[string]*model.PendingApproval),
		cooldowns: make(map[string]time.Time),
	}

	home, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(home, ".csm")
		_ = os.MkdirAll(dir, 0o755)
		m.autopilotPath = filepath.Join(dir, "autopilot.json")
		m.loadAutopilot()
	}

	return m
}

// RegisterSession adds or updates a session from a SessionStart hook.
func (m *Manager) RegisterSession(sid, cwd, permMode string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, exists := m.sessions[sid]
	if !exists {
		s = &model.Session{
			SessionID: sid,
			CWD:       cwd,
			State:     model.StateRunning,
		}
		m.sessions[sid] = s
	}
	s.CWD = cwd
	s.PermissionMode = permMode
	s.ProjectName = filepath.Base(cwd)

	// Restore persisted autopilot state.
	if ap, ok := m.autopilot[sid]; ok {
		s.Autopilot = ap
	}

	m.notifySubscribers()
}

// UnregisterSession removes a session (SessionEnd hook).
func (m *Manager) UnregisterSession(sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sid)
	// Clean up pending approval.
	if pa, ok := m.pending[sid]; ok {
		close(pa.ResponseCh)
		delete(m.pending, sid)
	}
	m.notifySubscribers()
}

// UpdateSessionFromScanner merges scanner-discovered session data.
func (m *Manager) UpdateSessionFromScanner(s *model.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.sessions[s.SessionID]
	if !ok {
		// New session from scanner.
		if ap, okAP := m.autopilot[s.SessionID]; okAP {
			s.Autopilot = ap
		}
		m.sessions[s.SessionID] = s
		m.notifySubscribers()
		return
	}

	// Merge: scanner provides richer data (activities, state, etc.)
	// but hook-registered data (CWD, permission_mode) takes priority.
	existing.State = s.State
	existing.Activities = s.Activities
	existing.LastText = s.LastText
	existing.LastActivity = s.LastActivity
	existing.PID = s.PID
	existing.TTY = s.TTY
	existing.GitBranch = s.GitBranch
	existing.JSONLPath = s.JSONLPath
	existing.Slug = s.Slug
	if existing.CWD == "" {
		existing.CWD = s.CWD
	}
	if existing.ProjectName == "" {
		existing.ProjectName = s.ProjectName
	}

	// Don't touch PendingTools from scanner — only hooks set real pending state.

	m.notifySubscribers()
}

// SetSlug updates the slug (display name) for a session.
func (m *Manager) SetSlug(sid, slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sid]; ok {
		if s.Slug != slug {
			s.Slug = slug
			m.notifySubscribers()
		}
	}
}

// SetGhosttyTab enriches a session with Ghostty tab info.
func (m *Manager) SetGhosttyTab(sid, tabName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sid]; ok {
		s.GhosttyTab = tabName
	}
}

// AddPending registers a new pending tool approval. Returns a channel
// that will receive the decision.
func (m *Manager) AddPending(sid string, tool model.PendingTool) <-chan model.ApprovalDecision {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Classify the tool.
	tool.Safety = classifier.ClassifyTool(tool.ToolName, tool.ToolInput)

	ch := make(chan model.ApprovalDecision, 1)
	pa := &model.PendingApproval{
		SessionID:  sid,
		Tool:       tool,
		ReceivedAt: time.Now(),
		ResponseCh: ch,
	}
	m.pending[sid] = pa

	// Update session's pending state.
	if s, ok := m.sessions[sid]; ok {
		s.PendingTools = append(s.PendingTools, tool)
		if tool.Safety == model.SafetyDestructive {
			s.HasDestructive = true
		}
	}

	m.notifySubscribers()
	return ch
}

// ResolvePending resolves the pending approval for a session.
func (m *Manager) ResolvePending(sid string, decision model.ApprovalDecision) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	pa, ok := m.pending[sid]
	if !ok {
		log.Printf("state: ResolvePending(%s) — no pending entry found (pending map has %d entries)", sid, len(m.pending))
		return false
	}

	// Check cooldown (prevent double-approve within a short window).
	if last, ok := m.cooldowns[sid]; ok && time.Since(last) < 500*time.Millisecond {
		log.Printf("state: cooldown active for session %s, skipping", sid)
		return false
	}

	select {
	case pa.ResponseCh <- decision:
	default:
	}
	delete(m.pending, sid)
	m.cooldowns[sid] = time.Now()

	// Clear session pending state.
	if s, ok := m.sessions[sid]; ok {
		s.PendingTools = nil
		s.HasDestructive = false
	}

	m.notifySubscribers()
	return true
}

// ShouldAutoApprove checks if autopilot should auto-approve a tool for this session.
func (m *Manager) ShouldAutoApprove(sid string, safety model.ToolSafety) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[sid]
	if !ok {
		return false
	}
	if !s.Autopilot {
		return false
	}
	// Only auto-approve safe and unknown tools. Destructive always needs manual.
	return safety != model.SafetyDestructive
}

// ToggleAutopilot toggles autopilot for a session, returning the new state.
func (m *Manager) ToggleAutopilot(sid string) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sid]
	if !ok {
		return false, false
	}

	s.Autopilot = !s.Autopilot
	m.autopilot[sid] = s.Autopilot
	m.saveAutopilot()

	// If autopilot just turned ON, approve any pending non-destructive tool.
	if s.Autopilot {
		if pa, ok := m.pending[sid]; ok {
			if pa.Tool.Safety != model.SafetyDestructive {
				select {
				case pa.ResponseCh <- model.DecisionAllow:
				default:
				}
				delete(m.pending, sid)
				m.cooldowns[sid] = time.Now()
				log.Printf("state: autopilot ON — auto-approved pending tool for %s", sid)
			}
		}
	}

	m.notifySubscribers()
	return s.Autopilot, true
}

// ApproveAllPending approves all non-destructive pending tools across all sessions.
func (m *Manager) ApproveAllPending() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for sid, pa := range m.pending {
		if pa.Tool.Safety == model.SafetyDestructive {
			continue
		}
		select {
		case pa.ResponseCh <- model.DecisionAllow:
		default:
		}
		delete(m.pending, sid)
		m.cooldowns[sid] = time.Now()
		count++
	}
	if count > 0 {
		m.notifySubscribers()
	}
	return count
}

// GetSessions returns a snapshot of all sessions sorted by state then session ID.
// Dead sessions with PID==0 are filtered out.
func (m *Manager) GetSessions() []model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]model.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		// Filter out dead sessions with no PID.
		if s.State == model.StateDead && s.PID == 0 {
			continue
		}
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		oi := stateOrder[result[i].State]
		oj := stateOrder[result[j].State]
		if oi != oj {
			return oi < oj
		}
		return result[i].SessionID < result[j].SessionID
	})

	return result
}

// GetPending returns the pending approval for a session, if any.
func (m *Manager) GetPending(sid string) *model.PendingApproval {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pending[sid]
}

// Subscribe returns a channel that receives a signal on every state change.
// The caller must read from the channel or risk blocking notifications.
func (m *Manager) Subscribe() chan struct{} {
	ch := make(chan struct{}, 16)
	m.subMu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (m *Manager) Unsubscribe(ch chan struct{}) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (m *Manager) notifySubscribers() {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- struct{}{}:
		default:
			// Subscriber is full, skip (non-blocking).
		}
	}
}

func (m *Manager) loadAutopilot() {
	data, err := os.ReadFile(m.autopilotPath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &m.autopilot)
}

func (m *Manager) saveAutopilot() {
	data, err := json.Marshal(m.autopilot)
	if err != nil {
		log.Printf("failed to marshal autopilot state: %v", err)
		return
	}
	if err := os.WriteFile(m.autopilotPath, data, 0o644); err != nil {
		log.Printf("failed to save autopilot state: %v", err)
	}
}
