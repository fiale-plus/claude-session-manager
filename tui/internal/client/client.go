// Package client connects to the CSM daemon over a Unix socket
// and provides methods for subscribing to session updates and
// sending control commands.
package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Session mirrors the daemon's session model.
type Session struct {
	SessionID      string        `json:"session_id"`
	Slug           string        `json:"slug,omitempty"`
	CWD            string        `json:"cwd"`
	ProjectName    string        `json:"project_name"`
	State          string        `json:"state"`
	AutopilotMode  string        `json:"autopilot_mode"`
	HasDestructive bool          `json:"has_destructive"`
	PendingTools   []PendingTool `json:"pending_tools,omitempty"`
	GhosttyTab      string       `json:"ghostty_tab,omitempty"`
	GhosttyTabIndex int          `json:"ghostty_tab_index"`
	GitBranch      string        `json:"git_branch,omitempty"`
	LastText       string        `json:"last_text,omitempty"`
	Activities     []Activity    `json:"activities,omitempty"`
	LastActivity   *time.Time    `json:"last_activity_time,omitempty"`
	PID            int           `json:"pid,omitempty"`
	PermissionMode string        `json:"permission_mode,omitempty"`
}

// PendingTool is a tool call awaiting approval.
type PendingTool struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	Safety    string         `json:"safety"`
}

// Activity is a timeline entry.
type Activity struct {
	Timestamp    time.Time `json:"timestamp"`
	ActivityType string    `json:"activity_type"`
	Summary      string    `json:"summary"`
	Detail       string    `json:"detail,omitempty"`
}

// TrackedPR mirrors the daemon's PR model.
type TrackedPR struct {
	Owner       string    `json:"owner"`
	Repo        string    `json:"repo"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HeadBranch  string    `json:"head_branch"`
	BaseBranch  string    `json:"base_branch"`
	URL         string    `json:"url"`
	State       string    `json:"state"`
	Checks      []PRCheck `json:"checks"`
	Reviews     []PRReview `json:"reviews"`
	Mergeable   string    `json:"mergeable"`
	Additions   int       `json:"additions"`
	Deletions   int       `json:"deletions"`
	CommitCount int       `json:"commit_count"`
	AutoMerge   bool      `json:"auto_merge"`
	Hammer      bool      `json:"hammer"`
	HammerCount int       `json:"hammer_count"`
	CreatedAt   time.Time `json:"created_at"`
	Timeline    []PREvent `json:"timeline"`
}

type PRCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Detail     string `json:"detail"`
	Duration   string `json:"duration"`
}

type PRReview struct {
	Author string `json:"author"`
	State  string `json:"state"`
	Body   string `json:"body"`
	At     string `json:"at"`
}

type PREvent struct {
	Time    time.Time `json:"time"`
	Icon    string    `json:"icon"`
	Message string    `json:"message"`
}

// serverEvent is the shape of NDJSON messages from the daemon.
type serverEvent struct {
	Event    string      `json:"event,omitempty"`
	Sessions []Session   `json:"sessions,omitempty"`
	PRs      []TrackedPR `json:"prs,omitempty"`
	OK       *bool       `json:"ok,omitempty"`
}

// request is the shape of NDJSON messages sent to the daemon.
type request struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`
	PRKey     string `json:"pr_key,omitempty"`
}

// Client manages the connection to the CSM daemon.
type Client struct {
	socketPath string

	mu   sync.Mutex
	conn net.Conn
}

// New creates a client targeting the given Unix socket path.
func New(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// StateUpdate carries both sessions and PRs from a subscribe event.
type StateUpdate struct {
	Sessions []Session
	PRs      []TrackedPR
}

// Subscribe connects to the daemon and streams state updates.
func (c *Client) Subscribe() (<-chan StateUpdate, error) {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	if err := c.writeRequest(request{Action: "subscribe"}); err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan StateUpdate, 16)
	go func() {
		defer close(ch)
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			var ev serverEvent
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
				log.Printf("client: unmarshal error: %v", err)
				continue
			}
			if ev.Event == "state_updated" || ev.Event == "sessions_updated" {
				ch <- StateUpdate{Sessions: ev.Sessions, PRs: ev.PRs}
			}
		}
	}()

	return ch, nil
}

// sendCommand opens a separate short-lived connection for a command,
// waits for the server response, and returns whether it succeeded.
func (c *Client) sendCommand(req request) error {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Set a deadline so we don't hang forever.
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		return err
	}

	// Read the server response to ensure it was processed.
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		var resp serverEvent
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			return err
		}
		if resp.OK != nil && !*resp.OK {
			return fmt.Errorf("command rejected by daemon")
		}
	}

	return nil
}

// ToggleAutopilot toggles autopilot for the given session.
func (c *Client) ToggleAutopilot(sessionID string) error {
	return c.sendCommand(request{Action: "toggle_autopilot", SessionID: sessionID})
}

// Approve approves the pending tool for the given session.
func (c *Client) Approve(sessionID string) error {
	return c.sendCommand(request{Action: "approve", SessionID: sessionID})
}

// Reject rejects the pending tool for the given session.
func (c *Client) Reject(sessionID string) error {
	return c.sendCommand(request{Action: "reject", SessionID: sessionID})
}

// ApproveAll approves all non-destructive pending tools across all sessions.
func (c *Client) ApproveAll() error {
	return c.sendCommand(request{Action: "approve_all"})
}

// AddPR adds a PR to tracking by URL.
func (c *Client) AddPR(url string) error {
	return c.sendCommand(request{Action: "add_pr", PRURL: url})
}

// RemovePR removes a PR from tracking by key (owner/repo#number).
func (c *Client) RemovePR(key string) error {
	return c.sendCommand(request{Action: "remove_pr", PRKey: key})
}

// Focus focuses the Ghostty tab for the given session.
func (c *Client) Focus(sessionID string) error {
	return c.sendCommand(request{Action: "focus", SessionID: sessionID})
}

// Close tears down the subscribe connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) writeRequest(req request) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return net.ErrClosed
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}
