package pr

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Poller fetches PR data from GitHub via the gh CLI.
type Poller struct {
	mu       sync.RWMutex
	tracked  map[string]*TrackedPR // "owner/repo#number" → PR
	onChange func()                // called when PR state changes

	storePath string // persistence path (~/.csm/prs.json)
}

// NewPoller creates a PR poller.
func NewPoller(storePath string, onChange func()) *Poller {
	p := &Poller{
		tracked:   make(map[string]*TrackedPR),
		onChange:  onChange,
		storePath: storePath,
	}
	p.load()
	return p
}

// Add starts tracking a PR.
func (p *Poller) Add(owner, repo string, number int) *TrackedPR {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	p.mu.Lock()
	defer p.mu.Unlock()

	if pr, ok := p.tracked[key]; ok {
		return pr
	}

	pr := &TrackedPR{
		Owner:         owner,
		Repo:          repo,
		Number:        number,
		AutopilotMode: PRAuto,
		Hammer:        true,
		MaxHammer:     3,
		MergeMethod:   "squash",
		Timeline:      []PREvent{{Time: time.Now(), Icon: "📝", Message: "Added to tracking"}},
	}
	p.tracked[key] = pr
	p.save()
	if p.onChange != nil {
		p.onChange()
	}
	return pr
}

// AddFromURL parses a GitHub PR URL and starts tracking.
func (p *Poller) AddFromURL(url string) (*TrackedPR, error) {
	// Parse: https://github.com/owner/repo/pull/123
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	parts := strings.Split(url, "/")
	if len(parts) < 5 || parts[len(parts)-2] != "pull" {
		return nil, fmt.Errorf("invalid PR URL: %s", url)
	}
	owner := parts[len(parts)-4]
	repo := parts[len(parts)-3]
	var number int
	if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &number); err != nil {
		return nil, fmt.Errorf("invalid PR number in URL: %s", url)
	}
	return p.Add(owner, repo, number), nil
}

// Remove stops tracking a PR.
func (p *Poller) Remove(owner, repo string, number int) {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.tracked, key)
	p.save()
	if p.onChange != nil {
		p.onChange()
	}
}

// GetAll returns all tracked PRs.
func (p *Poller) GetAll() []TrackedPR {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]TrackedPR, 0, len(p.tracked))
	for _, pr := range p.tracked {
		result = append(result, *pr)
	}
	return result
}

// CycleAutopilot cycles PR autopilot: off → auto → yolo → off.
func (p *Poller) CycleAutopilot(owner, repo string, number int) string {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
	p.mu.Lock()
	defer p.mu.Unlock()

	pr, ok := p.tracked[key]
	if !ok {
		return ""
	}

	switch pr.AutopilotMode {
	case PRAuto:
		pr.AutopilotMode = PRYolo
	case PRYolo:
		pr.AutopilotMode = PROff
	default:
		pr.AutopilotMode = PRAuto
	}
	pr.Timeline = append(pr.Timeline, PREvent{
		Time:    time.Now(),
		Icon:    "⚙",
		Message: fmt.Sprintf("Autopilot → %s", pr.AutopilotMode),
	})
	p.save()
	if p.onChange != nil {
		p.onChange()
	}
	return pr.AutopilotMode
}

// FailingCount returns how many PRs have failing checks.
func (p *Poller) FailingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for _, pr := range p.tracked {
		if pr.State == StateChecksFailing {
			n++
		}
	}
	return n
}

// Poll fetches latest state for all tracked PRs from GitHub.
func (p *Poller) Poll() {
	log.Printf("pr: polling %d tracked PRs (gh=%s)", len(p.tracked), ghBin())
	p.mu.RLock()
	keys := make([]string, 0, len(p.tracked))
	for k := range p.tracked {
		keys = append(keys, k)
	}
	p.mu.RUnlock()

	polled := false
	for _, key := range keys {
		p.mu.RLock()
		pr, ok := p.tracked[key]
		if !ok {
			p.mu.RUnlock()
			continue
		}
		owner, repo, number := pr.Owner, pr.Repo, pr.Number
		p.mu.RUnlock()

		p.pollOne(owner, repo, number)
		polled = true
	}
	if polled {
		p.mu.Lock()
		p.save()
		p.mu.Unlock()
		if p.onChange != nil {
			p.onChange()
		}
	}
}

func (p *Poller) pollOne(owner, repo string, number int) bool {
	key := fmt.Sprintf("%s/%s#%d", owner, repo, number)

	// Fetch PR data.
	out, err := exec.Command(ghBin(), "pr", "view",
		fmt.Sprintf("%d", number),
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "title,headRefName,baseRefName,url,state,statusCheckRollup,reviews,latestReviews,mergeable,additions,deletions,commits,isDraft,updatedAt",
	).Output()
	if err != nil {
		log.Printf("pr: gh pr view %s failed: %v", key, err)
		return false
	}

	var ghPR ghPRData
	if err := json.Unmarshal(out, &ghPR); err != nil {
		log.Printf("pr: parse %s: %v", key, err)
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	pr, ok := p.tracked[key]
	if !ok {
		return false
	}

	oldState := pr.State

	// Update fields.
	pr.Title = ghPR.Title
	pr.HeadBranch = ghPR.HeadRefName
	pr.BaseBranch = ghPR.BaseRefName
	pr.URL = ghPR.URL
	pr.Mergeable = ghPR.Mergeable
	pr.Additions = ghPR.Additions
	pr.Deletions = ghPR.Deletions
	pr.CommitCount = len(ghPR.Commits)
	pr.IsDraft = ghPR.IsDraft
	pr.UpdatedAt = ghPR.UpdatedAt

	// Parse checks.
	pr.Checks = nil
	for _, c := range ghPR.StatusCheckRollup {
		check := Check{
			Name:       c.Name,
			Status:     c.Status,
			Conclusion: c.Conclusion,
		}
		if c.CompletedAt != "" && c.StartedAt != "" {
			// Simple duration calc.
			start, _ := time.Parse(time.RFC3339, c.StartedAt)
			end, _ := time.Parse(time.RFC3339, c.CompletedAt)
			if !start.IsZero() && !end.IsZero() {
				d := end.Sub(start)
				if d < time.Minute {
					check.Duration = fmt.Sprintf("%ds", int(d.Seconds()))
				} else {
					check.Duration = fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
				}
			}
		}
		pr.Checks = append(pr.Checks, check)
	}

	// Parse reviews.
	pr.Reviews = nil
	for _, r := range ghPR.LatestReviews {
		review := Review{
			Author: r.Author.Login,
			State:  r.State,
			Body:   r.Body,
		}
		if !r.SubmittedAt.IsZero() {
			ago := time.Since(r.SubmittedAt)
			if ago < time.Hour {
				review.At = fmt.Sprintf("%dm ago", int(ago.Minutes()))
			} else if ago < 24*time.Hour {
				review.At = fmt.Sprintf("%dh ago", int(ago.Hours()))
			} else {
				review.At = fmt.Sprintf("%dd ago", int(ago.Hours()/24))
			}
		}
		pr.Reviews = append(pr.Reviews, review)
	}

	// Determine state.
	if ghPR.State == "MERGED" {
		pr.State = StateMerged
	} else if ghPR.State == "CLOSED" {
		pr.State = StateClosed
	} else {
		hasRunning := false
		hasFailing := false
		for _, c := range pr.Checks {
			if c.Status == "IN_PROGRESS" || c.Status == "QUEUED" {
				hasRunning = true
			}
			if c.Conclusion == "FAILURE" {
				hasFailing = true
			}
		}

		// Check if approved.
		hasApproval := false
		for _, r := range pr.Reviews {
			if r.State == "APPROVED" {
				hasApproval = true
				break
			}
		}

		switch {
		case hasFailing:
			pr.State = StateChecksFailing
		case hasRunning:
			pr.State = StateChecksRunning
		case hasApproval:
			pr.State = StateApproved
		default:
			pr.State = StateChecksPassing
		}
	}

	// Timeline events on state change (including first detection).
	if pr.State != oldState {
		pr.Timeline = append(pr.Timeline, PREvent{
			Time:    time.Now(),
			Icon:    stateIcon(pr.State),
			Message: fmt.Sprintf("State: %s → %s", oldState, pr.State),
		})
	}

	// Auto-merge if conditions met.
	if pr.ShouldAutoMerge() {
		go p.triggerMerge(pr)
	}

	// Auto-hammer if CI failing and hammer mode on.
	if pr.ShouldHammer() && pr.State == StateChecksFailing && pr.State != oldState {
		pr.HammerCount++
		pr.Timeline = append(pr.Timeline, PREvent{
			Time:    time.Now(),
			Icon:    "🔨",
			Message: fmt.Sprintf("Hammer attempt %d/%d", pr.HammerCount, pr.MaxHammer),
		})
		log.Printf("pr: hammer %s/%s#%d attempt %d", pr.Owner, pr.Repo, pr.Number, pr.HammerCount)
		// TODO: spawn fix-CI agent here
	}

	return pr.State != oldState
}

func (p *Poller) triggerMerge(pr *TrackedPR) {
	key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
	repo := fmt.Sprintf("%s/%s", pr.Owner, pr.Repo)
	num := fmt.Sprintf("%d", pr.Number)

	var err error
	switch pr.MergeMethod {
	case "aviator":
		err = exec.Command(ghBin(), "pr", "comment", num,
			"--repo", repo, "--body", "/aviator merge").Run()
	case "rebase":
		err = exec.Command(ghBin(), "pr", "merge", num,
			"--repo", repo, "--rebase", "--auto").Run()
	case "merge":
		err = exec.Command(ghBin(), "pr", "merge", num,
			"--repo", repo, "--merge", "--auto").Run()
	default: // squash
		err = exec.Command(ghBin(), "pr", "merge", num,
			"--repo", repo, "--squash", "--auto").Run()
	}

	p.mu.Lock()
	if tracked, ok := p.tracked[key]; ok {
		if err != nil {
			tracked.Timeline = append(tracked.Timeline, PREvent{
				Time: time.Now(), Icon: "✗", Message: "Auto-merge failed: " + err.Error(),
			})
			log.Printf("pr: auto-merge %s failed: %v", key, err)
		} else {
			tracked.Timeline = append(tracked.Timeline, PREvent{
				Time: time.Now(), Icon: "🚀", Message: "Auto-merge triggered (" + pr.MergeMethod + ")",
			})
			log.Printf("pr: auto-merge %s triggered (%s)", key, pr.MergeMethod)
		}
		p.save()
	}
	p.mu.Unlock()
	if p.onChange != nil {
		p.onChange()
	}
}

func stateIcon(s PRState) string {
	switch s {
	case StateChecksFailing:
		return "✗"
	case StateChecksRunning:
		return "⏳"
	case StateChecksPassing:
		return "✓"
	case StateApproved:
		return "✅"
	case StateMerged:
		return "🚀"
	case StateClosed:
		return "⊘"
	default:
		return "•"
	}
}

// RunLoop polls PRs on a timer.
func RunLoop(p *Poller, interval time.Duration, stop <-chan struct{}) {
	// Initial poll.
	p.Poll()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.Poll()
		case <-stop:
			return
		}
	}
}

// --- gh JSON response types ---

type ghPRData struct {
	Title              string           `json:"title"`
	HeadRefName        string           `json:"headRefName"`
	BaseRefName        string           `json:"baseRefName"`
	URL                string           `json:"url"`
	State              string           `json:"state"` // "OPEN", "MERGED", "CLOSED"
	Mergeable          string           `json:"mergeable"`
	Additions          int              `json:"additions"`
	Deletions          int              `json:"deletions"`
	IsDraft            bool             `json:"isDraft"`
	UpdatedAt          time.Time        `json:"updatedAt"`
	StatusCheckRollup  []ghCheck        `json:"statusCheckRollup"`
	LatestReviews      []ghReview       `json:"latestReviews"`
	Commits            []ghCommit       `json:"commits"`
}

type ghCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
}

type ghReview struct {
	Author      ghAuthor  `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghCommit struct{}

// ghBin returns the path to the gh CLI binary.
func ghBin() string {
	// Try common paths for launchd context where PATH is minimal.
	for _, p := range []string{
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fallback to PATH lookup.
	if p, err := exec.LookPath("gh"); err == nil {
		return p
	}
	return "gh"
}

// --- persistence ---

func (p *Poller) load() {
	data, err := os.ReadFile(p.storePath)
	if err != nil {
		return
	}
	var prs map[string]*TrackedPR
	if err := json.Unmarshal(data, &prs); err != nil {
		return
	}
	p.tracked = prs
}

func (p *Poller) save() {
	data, err := json.MarshalIndent(p.tracked, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p.storePath, data, 0o644)
}
