// Package notify provides macOS desktop notifications for session state transitions.
package notify

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

const rateLimitSecs = 30.0

// Notifier tracks state transitions and fires notifications.
type Notifier struct {
	mu            sync.Mutex
	prevState     map[string]model.SessionState
	prevDestr     map[string]bool
	lastNotified  map[string]time.Time
	rateLimit     time.Duration
}

// New creates a notifier.
func New() *Notifier {
	return &Notifier{
		prevState:    make(map[string]model.SessionState),
		prevDestr:    make(map[string]bool),
		lastNotified: make(map[string]time.Time),
		rateLimit:    time.Duration(rateLimitSecs * float64(time.Second)),
	}
}

// Check inspects sessions and fires notifications for state transitions.
func (n *Notifier) Check(sessions []model.Session) {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()

	for _, s := range sessions {
		sid := s.SessionID
		name := s.ProjectName
		if name == "" {
			name = s.Slug
		}
		if name == "" {
			name = sid
		}

		prev := n.prevState[sid]

		// RUNNING → WAITING.
		if prev == model.StateRunning && s.State == model.StateWaiting {
			n.maybeNotify(sid, now,
				"Session needs input",
				fmt.Sprintf("%s: Claude finished — waiting for you", name),
			)
		}

		// Autopilot blocked by destructive tool.
		prevD := n.prevDestr[sid]
		if s.Autopilot && s.HasDestructive && !prevD {
			n.maybeNotify(sid, now,
				"Manual approval needed",
				fmt.Sprintf("%s: destructive tool call needs your approval", name),
			)
		}

		n.prevState[sid] = s.State
		n.prevDestr[sid] = s.HasDestructive
	}
}

func (n *Notifier) maybeNotify(sid string, now time.Time, title, message string) {
	if last, ok := n.lastNotified[sid]; ok && now.Sub(last) < n.rateLimit {
		return
	}
	sendNotification(title, message)
	n.lastNotified[sid] = now
}

func sendNotification(title, message string) {
	safeTitle := strings.ReplaceAll(strings.ReplaceAll(title, `\`, `\\`), `"`, `\"`)
	safeMsg := strings.ReplaceAll(strings.ReplaceAll(message, `\`, `\\`), `"`, `\"`)

	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "default"`, safeMsg, safeTitle)

	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		log.Printf("notify: failed to send notification: %v", err)
	}
}
