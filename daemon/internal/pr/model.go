// Package pr manages tracked pull requests — polling, state, and lifecycle.
package pr

import "time"

// PRState represents the current state of a tracked PR.
type PRState string

const (
	StateChecksRunning PRState = "checks_running"
	StateChecksFailing PRState = "checks_failing"
	StateChecksPassing PRState = "checks_passing"
	StateApproved      PRState = "approved"
	StateMerged        PRState = "merged"
	StateClosed        PRState = "closed"
)

// Check represents a CI check result.
type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // "COMPLETED", "IN_PROGRESS", "QUEUED"
	Conclusion string `json:"conclusion"` // "SUCCESS", "FAILURE", "NEUTRAL", ""
	Detail     string `json:"detail"`     // failure message if available
	Duration   string `json:"duration"`   // human-readable
}

// Review represents a PR reviewer.
type Review struct {
	Author  string `json:"author"`
	State   string `json:"state"` // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "PENDING"
	Body    string `json:"body"`
	At      string `json:"at"` // human-readable time
}

// PREvent is a timeline entry for the PR.
type PREvent struct {
	Time    time.Time `json:"time"`
	Icon    string    `json:"icon"`
	Message string    `json:"message"`
}

// TrackedPR represents a PR being monitored by the daemon.
type TrackedPR struct {
	Owner      string    `json:"owner"`
	Repo       string    `json:"repo"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	HeadBranch string    `json:"head_branch"`
	BaseBranch string    `json:"base_branch"`
	URL        string    `json:"url"`
	State      PRState   `json:"state"`
	Checks     []Check   `json:"checks"`
	Reviews    []Review  `json:"reviews"`
	Mergeable  string    `json:"mergeable"` // "MERGEABLE", "CONFLICTING", "UNKNOWN"
	Additions  int       `json:"additions"`
	Deletions  int       `json:"deletions"`
	CommitCount int      `json:"commit_count"`
	IsDraft    bool      `json:"is_draft"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Timeline   []PREvent `json:"timeline"`

	// Tracking config
	AutopilotMode  string `json:"autopilot_mode"`  // "off", "auto", "yolo"
	Hammer         bool   `json:"hammer"`           // auto-fix CI failures
	HammerCount    int    `json:"hammer_count"`     // fix attempts so far
	MaxHammer      int    `json:"max_hammer"`       // max fix attempts (default 3)
	MergeMethod    string `json:"merge_method"`     // "squash", "merge", "rebase", "aviator", "" = unset
	MergeTriggered bool   `json:"merge_triggered"`  // true once auto-merge has been fired; resets on check regression
	RunReview      bool   `json:"run_review"`       // run code-review skill on creation
}

// PR autopilot modes.
const (
	PRAuto = "auto" // hammer CI + auto-merge on approval + green checks
	PRYolo = "yolo" // auto mode + auto-approve (no human review needed)
	PROff  = "off"  // manual everything
)

// ShouldAutoMerge returns true if the PR should be auto-merged now.
func (pr *TrackedPR) ShouldAutoMerge() bool {
	if pr.AutopilotMode == PROff {
		return false
	}
	if pr.State == StateMerged || pr.State == StateClosed {
		return false
	}
	if pr.Mergeable != "MERGEABLE" {
		return false
	}
	// Merge method must be configured — auto-merge is blocked until the user picks one.
	if pr.MergeMethod == "" {
		return false
	}

	// No check may be failing.
	for _, c := range pr.Checks {
		if c.Conclusion == "FAILURE" {
			return false
		}
	}

	if pr.AutopilotMode == PRYolo {
		// YOLO: no checks required, no approval required.
		// Repos with no CI (empty Checks) can still be merged.
		return true
	}

	// AUTO: at least one completed check required.
	hasCompleted := false
	for _, c := range pr.Checks {
		if c.Status == "COMPLETED" {
			hasCompleted = true
			break
		}
	}
	if !hasCompleted {
		return false
	}

	// AUTO: needs at least one approval.
	for _, r := range pr.Reviews {
		if r.State == "APPROVED" {
			return true
		}
	}
	return false
}

// ShouldHammer returns true if the daemon should spawn a fix-CI agent.
func (pr *TrackedPR) ShouldHammer() bool {
	if !pr.Hammer {
		return false
	}
	if pr.AutopilotMode == PROff {
		return false
	}
	maxAttempts := pr.MaxHammer
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	if pr.HammerCount >= maxAttempts {
		return false
	}
	return pr.HasFailingChecks()
}

// ChecksSummary returns (passing, total) counts.
func (pr *TrackedPR) ChecksSummary() (int, int) {
	passing := 0
	for _, c := range pr.Checks {
		if c.Conclusion == "SUCCESS" || c.Conclusion == "NEUTRAL" {
			passing++
		}
	}
	return passing, len(pr.Checks)
}

// HasFailingChecks returns true if any check has failed.
func (pr *TrackedPR) HasFailingChecks() bool {
	for _, c := range pr.Checks {
		if c.Conclusion == "FAILURE" {
			return true
		}
	}
	return false
}

// NeedsAttention returns true if the PR needs user action.
func (pr *TrackedPR) NeedsAttention() bool {
	return pr.State == StateChecksFailing
}
