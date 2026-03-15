package model

import "time"

// SessionState represents the current state of a Claude Code session.
type SessionState string

const (
	StateRunning SessionState = "running"
	StateWaiting SessionState = "waiting"
	StateIdle    SessionState = "idle"
	StateDead    SessionState = "dead"
)

// ActivityType categorizes session activities.
type ActivityType string

const (
	ActivityToolUse     ActivityType = "tool_use"
	ActivityText        ActivityType = "text"
	ActivityThinking    ActivityType = "thinking"
	ActivityUserMessage ActivityType = "user_message"
	ActivitySystem      ActivityType = "system"
)

// Activity represents a single event in a session timeline.
type Activity struct {
	Timestamp    time.Time    `json:"timestamp"`
	ActivityType ActivityType `json:"activity_type"`
	Summary      string       `json:"summary"`
	Detail       string       `json:"detail,omitempty"`
}

// Autopilot modes.
const (
	AutopilotOff  = "off"
	AutopilotOn   = "on"   // auto-approve safe+unknown, block destructive
	AutopilotYolo = "yolo" // auto-approve all, grace period for destructive
)

// Session represents a Claude Code session.
type Session struct {
	SessionID    string       `json:"session_id"`
	Slug         string       `json:"slug,omitempty"`
	CWD          string       `json:"cwd"`
	ProjectName  string       `json:"project_name"`
	JSONLPath    string       `json:"jsonl_path,omitempty"`
	State        SessionState `json:"state"`
	LastActivity *time.Time   `json:"last_activity_time,omitempty"`
	Activities   []Activity   `json:"activities,omitempty"`
	LastText     string       `json:"last_text,omitempty"`
	GhosttyTab   string       `json:"ghostty_tab,omitempty"`
	PID          int          `json:"pid,omitempty"`
	TTY          string       `json:"tty,omitempty"`
	GitBranch    string       `json:"git_branch,omitempty"`
	// AutopilotMode: "off", "on" (safe only), "yolo" (all, with grace period for destructive)
	AutopilotMode string `json:"autopilot_mode"`

	// Derived fields (set by state manager)
	HasDestructive bool           `json:"has_destructive"`
	PendingTools   []PendingTool  `json:"pending_tools,omitempty"`
	PermissionMode string         `json:"permission_mode,omitempty"`
}
