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
	AutoMerge   bool   `json:"auto_merge"`
	Hammer      bool   `json:"hammer"`      // auto-fix CI failures
	HammerCount int    `json:"hammer_count"` // fix attempts so far
	MergeMethod string `json:"merge_method"` // "squash", "merge", "rebase"
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
