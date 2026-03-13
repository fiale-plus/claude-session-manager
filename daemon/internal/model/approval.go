package model

import "time"

// ToolSafety classifies how dangerous a tool call is.
type ToolSafety string

const (
	SafetySafe        ToolSafety = "safe"
	SafetyDestructive ToolSafety = "destructive"
	SafetyUnknown     ToolSafety = "unknown"
)

// PendingTool represents a tool call waiting for approval.
type PendingTool struct {
	ToolName  string            `json:"tool_name"`
	ToolInput map[string]any    `json:"tool_input"`
	Safety    ToolSafety        `json:"safety"`
}

// PendingApproval tracks a PreToolUse hook waiting for a decision.
type PendingApproval struct {
	SessionID string
	Tool      PendingTool
	ReceivedAt time.Time
	// ResponseCh is used to send the decision back to the hook handler.
	ResponseCh chan ApprovalDecision
}

// ApprovalDecision is the daemon's response to a PreToolUse hook.
type ApprovalDecision string

const (
	DecisionAllow     ApprovalDecision = "allow"
	DecisionDeny      ApprovalDecision = "deny"
	DecisionPassthrough ApprovalDecision = "passthrough" // let CC handle it
)
