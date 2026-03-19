package pr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newTestPoller creates a poller with a temp store path and no onChange callback.
func newTestPoller(t *testing.T) *Poller {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "prs.json")
	return NewPoller(storePath, nil)
}

// newTestPollerWithCallback creates a poller with a change callback counter.
func newTestPollerWithCallback(t *testing.T) (*Poller, *int) {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "prs.json")
	count := 0
	p := NewPoller(storePath, func() { count++ })
	return p, &count
}

// === Add ===

func TestAdd_New(t *testing.T) {
	p := newTestPoller(t)
	pr, _ := p.Add("octocat", "hello-world", 42)

	if pr.Owner != "octocat" {
		t.Errorf("owner = %q, want octocat", pr.Owner)
	}
	if pr.Repo != "hello-world" {
		t.Errorf("repo = %q, want hello-world", pr.Repo)
	}
	if pr.Number != 42 {
		t.Errorf("number = %d, want 42", pr.Number)
	}
	if pr.AutopilotMode != PRAuto {
		t.Errorf("autopilot = %q, want auto", pr.AutopilotMode)
	}
	if !pr.Hammer {
		t.Error("hammer should default to true")
	}
	if pr.MaxHammer != 3 {
		t.Errorf("max_hammer = %d, want 3", pr.MaxHammer)
	}
	if pr.MergeMethod != "" {
		t.Errorf("merge_method = %q, want empty (unset for new repo)", pr.MergeMethod)
	}
	if !pr.ReviewEnabled {
		t.Error("ReviewEnabled should default to true")
	}
	if len(pr.Timeline) != 1 {
		t.Errorf("timeline should have 1 event, got %d", len(pr.Timeline))
	}
}

func TestAdd_Duplicate(t *testing.T) {
	p := newTestPoller(t)
	pr1, _ := p.Add("octocat", "repo", 1)
	pr2, _ := p.Add("octocat", "repo", 1)

	if pr1 != pr2 {
		t.Error("adding same PR twice should return the same pointer")
	}
	all := p.GetAll()
	if len(all) != 1 {
		t.Errorf("should have 1 PR, got %d", len(all))
	}
}

func TestAdd_Multiple(t *testing.T) {
	p := newTestPoller(t)
	p.Add("octocat", "repo", 1)
	p.Add("octocat", "repo", 2)
	p.Add("other", "project", 99)

	all := p.GetAll()
	if len(all) != 3 {
		t.Errorf("should have 3 PRs, got %d", len(all))
	}
}

func TestAdd_TriggersOnChange(t *testing.T) {
	p, count := newTestPollerWithCallback(t)
	p.Add("owner", "repo", 1)
	if *count != 1 {
		t.Errorf("onChange called %d times, want 1", *count)
	}
}

// === AddFromURL ===

func TestAddFromURL_Valid(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		owner  string
		repo   string
		number int
	}{
		{
			name:   "standard",
			url:    "https://github.com/octocat/hello-world/pull/42",
			owner:  "octocat",
			repo:   "hello-world",
			number: 42,
		},
		{
			name:   "trailing slash",
			url:    "https://github.com/owner/repo/pull/7/",
			owner:  "owner",
			repo:   "repo",
			number: 7,
		},
		{
			name:   "whitespace",
			url:    "  https://github.com/a/b/pull/1  ",
			owner:  "a",
			repo:   "b",
			number: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPoller(t)
			pr, _, err := p.AddFromURL(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pr.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", pr.Owner, tt.owner)
			}
			if pr.Repo != tt.repo {
				t.Errorf("repo = %q, want %q", pr.Repo, tt.repo)
			}
			if pr.Number != tt.number {
				t.Errorf("number = %d, want %d", pr.Number, tt.number)
			}
		})
	}
}

func TestAddFromURL_Invalid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"not a URL", "hello world"},
		{"no pull path", "https://github.com/owner/repo"},
		{"issue not pull", "https://github.com/owner/repo/issues/42"},
		{"no number", "https://github.com/owner/repo/pull/"},
		{"non-numeric", "https://github.com/owner/repo/pull/abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPoller(t)
			_, _, err := p.AddFromURL(tt.url)
			if err == nil {
				t.Errorf("expected error for URL %q", tt.url)
			}
		})
	}
}

// === Remove ===

func TestRemove(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1)
	p.Add("owner", "repo", 2)

	p.Remove("owner", "repo", 1)
	all := p.GetAll()
	if len(all) != 1 {
		t.Errorf("after remove: got %d PRs, want 1", len(all))
	}
	if all[0].Number != 2 {
		t.Errorf("remaining PR number = %d, want 2", all[0].Number)
	}
}

func TestRemove_Nonexistent(t *testing.T) {
	p := newTestPoller(t)
	// Should not panic.
	p.Remove("owner", "repo", 999)
}

func TestRemove_TriggersOnChange(t *testing.T) {
	p, count := newTestPollerWithCallback(t)
	p.Add("owner", "repo", 1)
	*count = 0 // Reset after Add callback.
	p.Remove("owner", "repo", 1)
	if *count != 1 {
		t.Errorf("onChange called %d times on remove, want 1", *count)
	}
}

// === CycleAutopilot ===

func TestCycleAutopilot_Cycle(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1) // Default: auto

	// auto -> yolo
	mode := p.CycleAutopilot("owner", "repo", 1)
	if mode != PRYolo {
		t.Errorf("cycle 1: got %q, want yolo", mode)
	}

	// yolo -> off
	mode = p.CycleAutopilot("owner", "repo", 1)
	if mode != PROff {
		t.Errorf("cycle 2: got %q, want off", mode)
	}

	// off -> auto
	mode = p.CycleAutopilot("owner", "repo", 1)
	if mode != PRAuto {
		t.Errorf("cycle 3: got %q, want auto", mode)
	}
}

func TestCycleAutopilot_Nonexistent(t *testing.T) {
	p := newTestPoller(t)
	mode := p.CycleAutopilot("owner", "repo", 999)
	if mode != "" {
		t.Errorf("nonexistent PR: got %q, want empty", mode)
	}
}

func TestCycleAutopilot_AddsTimelineEvent(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1)
	initialTimeline := len(p.tracked["owner/repo#1"].Timeline)

	p.CycleAutopilot("owner", "repo", 1)

	newTimeline := len(p.tracked["owner/repo#1"].Timeline)
	if newTimeline != initialTimeline+1 {
		t.Errorf("timeline events: got %d, want %d", newTimeline, initialTimeline+1)
	}
}

func TestCycleAutopilot_TriggersOnChange(t *testing.T) {
	p, count := newTestPollerWithCallback(t)
	p.Add("owner", "repo", 1)
	*count = 0
	p.CycleAutopilot("owner", "repo", 1)
	if *count != 1 {
		t.Errorf("onChange called %d times, want 1", *count)
	}
}

// === GetAll ===

func TestGetAll_Empty(t *testing.T) {
	p := newTestPoller(t)
	all := p.GetAll()
	if len(all) != 0 {
		t.Errorf("empty poller: got %d PRs, want 0", len(all))
	}
}

func TestGetAll_ReturnsCopies(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1)

	all := p.GetAll()
	all[0].Title = "modified"

	// Original should be unchanged.
	p.mu.RLock()
	original := p.tracked["owner/repo#1"]
	p.mu.RUnlock()
	if original.Title == "modified" {
		t.Error("GetAll should return copies, not references")
	}
}

// === FailingCount ===

func TestFailingCount_None(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1)
	if n := p.FailingCount(); n != 0 {
		t.Errorf("no failing: got %d, want 0", n)
	}
}

func TestFailingCount_Some(t *testing.T) {
	p := newTestPoller(t)
	pr1, _ := p.Add("owner", "repo", 1)
	pr1.State = StateChecksFailing
	pr2, _ := p.Add("owner", "repo", 2)
	pr2.State = StateChecksPassing
	pr3, _ := p.Add("owner", "repo", 3)
	pr3.State = StateChecksFailing

	if n := p.FailingCount(); n != 2 {
		t.Errorf("two failing: got %d, want 2", n)
	}
}

// === Persistence (save/load) ===

func TestPersistence_SaveAndLoad(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")

	// Create a poller and add PRs.
	p1 := NewPoller(storePath, nil)
	p1.Add("octocat", "hello-world", 42)
	pr2, _ := p1.Add("other", "project", 7)
	pr2.Title = "Test PR"

	// Manually set title via direct access (simulating pollOne).
	p1.mu.Lock()
	p1.tracked["other/project#7"].Title = "Test PR"
	p1.save()
	p1.mu.Unlock()

	// Create a new poller from the same file — should load.
	p2 := NewPoller(storePath, nil)
	all := p2.GetAll()
	if len(all) != 2 {
		t.Fatalf("loaded %d PRs, want 2", len(all))
	}

	// Verify data integrity.
	found := false
	for _, pr := range all {
		if pr.Owner == "other" && pr.Repo == "project" && pr.Number == 7 {
			found = true
			if pr.Title != "Test PR" {
				t.Errorf("loaded title = %q, want 'Test PR'", pr.Title)
			}
		}
	}
	if !found {
		t.Error("loaded PRs missing other/project#7")
	}
}

func TestPersistence_LoadEmpty(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "nonexistent.json")
	p := NewPoller(storePath, nil)
	all := p.GetAll()
	if len(all) != 0 {
		t.Errorf("load from nonexistent file: got %d PRs, want 0", len(all))
	}
}

func TestPersistence_LoadCorrupt(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	// Write corrupt JSON.
	_ = os.WriteFile(storePath, []byte("not valid json{{{"), 0o644)

	p := NewPoller(storePath, nil)
	all := p.GetAll()
	if len(all) != 0 {
		t.Errorf("load from corrupt file: got %d PRs, want 0", len(all))
	}
}

func TestPersistence_AddPersists(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("owner", "repo", 1)

	// Verify the file exists and contains the PR.
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("failed to read store file: %v", err)
	}
	var stored pollerStore
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("failed to parse store file: %v", err)
	}
	if _, ok := stored.PRs["owner/repo#1"]; !ok {
		t.Error("store file should contain owner/repo#1")
	}
}

func TestPersistence_RemovePersists(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("owner", "repo", 1)
	p.Add("owner", "repo", 2)
	p.Remove("owner", "repo", 1)

	// Reload and verify.
	p2 := NewPoller(storePath, nil)
	all := p2.GetAll()
	if len(all) != 1 {
		t.Errorf("after remove + reload: got %d PRs, want 1", len(all))
	}
}

func TestPersistence_CycleAutopilotPersists(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("owner", "repo", 1)
	p.CycleAutopilot("owner", "repo", 1) // auto -> yolo

	// Reload and verify.
	p2 := NewPoller(storePath, nil)
	all := p2.GetAll()
	if len(all) != 1 {
		t.Fatalf("reload: got %d PRs, want 1", len(all))
	}
	if all[0].AutopilotMode != PRYolo {
		t.Errorf("persisted autopilot = %q, want yolo", all[0].AutopilotMode)
	}
}

// === ghPRData parsing ===

func TestGhPRDataParsing(t *testing.T) {
	raw := `{
		"title": "Fix bug",
		"headRefName": "fix-branch",
		"baseRefName": "main",
		"url": "https://github.com/owner/repo/pull/1",
		"state": "OPEN",
		"mergeable": "MERGEABLE",
		"additions": 10,
		"deletions": 3,
		"isDraft": false,
		"statusCheckRollup": [
			{"name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS", "startedAt": "2024-01-01T00:00:00Z", "completedAt": "2024-01-01T00:05:00Z"},
			{"name": "lint", "status": "IN_PROGRESS", "conclusion": ""}
		],
		"latestReviews": [
			{"author": {"login": "reviewer"}, "state": "APPROVED", "body": "LGTM", "submittedAt": "2024-01-01T00:00:00Z"}
		],
		"commits": [{}]
	}`

	var pr ghPRData
	if err := json.Unmarshal([]byte(raw), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if pr.Title != "Fix bug" {
		t.Errorf("title = %q, want 'Fix bug'", pr.Title)
	}
	if pr.HeadRefName != "fix-branch" {
		t.Errorf("head = %q", pr.HeadRefName)
	}
	if pr.BaseRefName != "main" {
		t.Errorf("base = %q", pr.BaseRefName)
	}
	if pr.Mergeable != "MERGEABLE" {
		t.Errorf("mergeable = %q", pr.Mergeable)
	}
	if len(pr.StatusCheckRollup) != 2 {
		t.Errorf("checks = %d, want 2", len(pr.StatusCheckRollup))
	}
	if len(pr.LatestReviews) != 1 {
		t.Errorf("reviews = %d, want 1", len(pr.LatestReviews))
	}
	if pr.LatestReviews[0].Author.Login != "reviewer" {
		t.Errorf("reviewer = %q", pr.LatestReviews[0].Author.Login)
	}
	if len(pr.Commits) != 1 {
		t.Errorf("commits = %d, want 1", len(pr.Commits))
	}
	if pr.Additions != 10 {
		t.Errorf("additions = %d, want 10", pr.Additions)
	}
	if pr.Deletions != 3 {
		t.Errorf("deletions = %d, want 3", pr.Deletions)
	}
}

func TestGhPRDataParsing_MergedState(t *testing.T) {
	raw := `{"title":"PR","state":"MERGED","statusCheckRollup":[],"latestReviews":[],"commits":[]}`
	var pr ghPRData
	if err := json.Unmarshal([]byte(raw), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.State != "MERGED" {
		t.Errorf("state = %q, want MERGED", pr.State)
	}
}

func TestGhPRDataParsing_DraftPR(t *testing.T) {
	raw := `{"title":"Draft","isDraft":true,"state":"OPEN","statusCheckRollup":[],"latestReviews":[],"commits":[]}`
	var pr ghPRData
	if err := json.Unmarshal([]byte(raw), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !pr.IsDraft {
		t.Error("isDraft should be true")
	}
}

// === stateIcon ===

// === AddFromURL additional edge cases ===

func TestAddFromURL_MinimalPath(t *testing.T) {
	p := newTestPoller(t)
	// URL with minimal path segments but valid structure.
	pr, _, err := p.AddFromURL("https://github.com/a/b/pull/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Owner != "a" || pr.Repo != "b" || pr.Number != 1 {
		t.Errorf("parsed = %s/%s#%d, want a/b#1", pr.Owner, pr.Repo, pr.Number)
	}
}

func TestAddFromURL_LargeNumber(t *testing.T) {
	p := newTestPoller(t)
	pr, _, err := p.AddFromURL("https://github.com/a/b/pull/99999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 99999 {
		t.Errorf("number = %d, want 99999", pr.Number)
	}
}

// === Concurrent access ===

func TestConcurrentAddRemove(t *testing.T) {
	p := newTestPoller(t)
	done := make(chan struct{})
	// Add and remove concurrently — should not race.
	go func() {
		for i := 0; i < 100; i++ {
			p.Add("owner", "repo", i)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			p.Remove("owner", "repo", i)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			_ = p.GetAll()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	<-done
}

func TestConcurrentCycleAutopilot(t *testing.T) {
	p := newTestPoller(t)
	p.Add("owner", "repo", 1)
	done := make(chan struct{})
	for g := 0; g < 5; g++ {
		go func() {
			for i := 0; i < 20; i++ {
				p.CycleAutopilot("owner", "repo", 1)
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 5; g++ {
		<-done
	}
}

func TestConcurrentFailingCount(t *testing.T) {
	p := newTestPoller(t)
	pr, _ := p.Add("owner", "repo", 1)
	pr.State = StateChecksFailing
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = p.FailingCount()
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 50; i++ {
			_ = p.GetAll()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// === Persistence edge cases ===

func TestPersistence_EmptyStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	// Write empty JSON object.
	_ = os.WriteFile(storePath, []byte("{}"), 0o644)
	p := NewPoller(storePath, nil)
	all := p.GetAll()
	if len(all) != 0 {
		t.Errorf("empty store: got %d PRs, want 0", len(all))
	}
}

func TestPersistence_NullJSON(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	_ = os.WriteFile(storePath, []byte("null"), 0o644)
	p := NewPoller(storePath, nil)
	// Should not panic and should initialize empty.
	all := p.GetAll()
	if len(all) != 0 {
		t.Errorf("null JSON: got %d PRs, want 0", len(all))
	}
}

// === ghBin ===

func TestGhBin(t *testing.T) {
	// Should return a non-empty string regardless of environment.
	bin := ghBin()
	if bin == "" {
		t.Error("ghBin() should not return empty string")
	}
}

func TestStateIcon(t *testing.T) {
	tests := []struct {
		state PRState
		want  string
	}{
		{StateChecksFailing, "✗"},
		{StateChecksRunning, "⏳"},
		{StateChecksPassing, "✓"},
		{StateApproved, "✅"},
		{StateMerged, "🚀"},
		{StateClosed, "⊘"},
		{"unknown", "•"},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := stateIcon(tt.state); got != tt.want {
				t.Errorf("stateIcon(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}
