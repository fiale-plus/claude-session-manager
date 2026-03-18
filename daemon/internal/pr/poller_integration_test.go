package pr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- pollOne tests using mock gh binary ---
// We create a test-specific gh script that returns canned JSON.

func createMockGh(t *testing.T, response string) string {
	t.Helper()
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", response)
	if err := os.WriteFile(ghPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Override ghBinFunc so pollOne/triggerMerge use our mock.
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })
	return ghPath
}

func makeGhResponse(state string, checks []ghCheck, reviews []ghReview, mergeable string) string {
	data := ghPRData{
		Title:             "Test PR",
		HeadRefName:       "feature",
		BaseRefName:       "main",
		URL:               "https://github.com/test/repo/pull/1",
		State:             state,
		Mergeable:         mergeable,
		Additions:         10,
		Deletions:         3,
		IsDraft:           false,
		UpdatedAt:         time.Now(),
		StatusCheckRollup: checks,
		LatestReviews:     reviews,
		Commits:           []ghCommit{{}},
	}
	b, _ := json.Marshal(data)
	return string(b)
}

func TestPollOne_OpenWithPassingChecks(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{
			{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS",
				StartedAt: "2024-01-01T00:00:00Z", CompletedAt: "2024-01-01T00:05:00Z"},
		},
		nil,
		"MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	changed := p.pollOne("test", "repo", 1)
	if !changed {
		t.Error("pollOne should return true on first poll (state changed from empty)")
	}

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.Title != "Test PR" {
		t.Errorf("title = %q, want 'Test PR'", pr.Title)
	}
	if pr.HeadBranch != "feature" {
		t.Errorf("head = %q, want feature", pr.HeadBranch)
	}
	if pr.State != StateChecksPassing {
		t.Errorf("state = %q, want checks_passing", pr.State)
	}
	if len(pr.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(pr.Checks))
	}
	if pr.Checks[0].Duration == "" {
		t.Error("duration should be computed for completed checks")
	}
	if pr.CommitCount != 1 {
		t.Errorf("commits = %d, want 1", pr.CommitCount)
	}
}

func TestPollOne_OpenWithFailingChecks(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{
			{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"},
		},
		nil,
		"MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateChecksFailing {
		t.Errorf("state = %q, want checks_failing", pr.State)
	}
}

func TestPollOne_OpenWithRunningChecks(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{
			{Name: "ci", Status: "IN_PROGRESS", Conclusion: ""},
		},
		nil,
		"MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateChecksRunning {
		t.Errorf("state = %q, want checks_running", pr.State)
	}
}

func TestPollOne_Merged(t *testing.T) {
	resp := makeGhResponse("MERGED",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		nil,
		"MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateMerged {
		t.Errorf("state = %q, want merged", pr.State)
	}
}

func TestPollOne_Closed(t *testing.T) {
	resp := makeGhResponse("CLOSED", nil, nil, "")
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateClosed {
		t.Errorf("state = %q, want closed", pr.State)
	}
}

func TestPollOne_WithApproval(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{
			{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"},
		},
		[]ghReview{
			{Author: ghAuthor{Login: "reviewer"}, State: "APPROVED", Body: "LGTM",
				SubmittedAt: time.Now().Add(-30 * time.Minute)},
		},
		"MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateApproved {
		t.Errorf("state = %q, want approved", pr.State)
	}
	if len(pr.Reviews) != 1 {
		t.Fatalf("reviews = %d, want 1", len(pr.Reviews))
	}
	if pr.Reviews[0].Author != "reviewer" {
		t.Errorf("reviewer = %q", pr.Reviews[0].Author)
	}
	if pr.Reviews[0].At == "" {
		t.Error("review 'at' should be populated")
	}
}

func TestPollOne_ReviewTimeFormatting(t *testing.T) {
	// Test different time ranges for review.At formatting.
	tests := []struct {
		name     string
		ago      time.Duration
		contains string
	}{
		{"minutes", 30 * time.Minute, "m ago"},
		{"hours", 5 * time.Hour, "h ago"},
		{"days", 48 * time.Hour, "d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := makeGhResponse("OPEN",
				[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
				[]ghReview{
					{Author: ghAuthor{Login: "rev"}, State: "APPROVED",
						SubmittedAt: time.Now().Add(-tt.ago)},
				},
				"MERGEABLE",
			)
			createMockGh(t, resp)

			storePath := filepath.Join(t.TempDir(), "prs.json")
			p := NewPoller(storePath, nil)
			p.Add("test", "repo", 1)
			p.pollOne("test", "repo", 1)

			p.mu.RLock()
			pr := p.tracked["test/repo#1"]
			p.mu.RUnlock()

			if pr.Reviews[0].At == "" {
				t.Error("at should be populated")
			}
		})
	}
}

func TestPollOne_CheckDurationFormatting(t *testing.T) {
	tests := []struct {
		name      string
		startAt   string
		endAt     string
		wantShort bool // <1min → "Xs" format, else "Xm Xs"
	}{
		{"short", "2024-01-01T00:00:00Z", "2024-01-01T00:00:30Z", true},
		{"long", "2024-01-01T00:00:00Z", "2024-01-01T00:05:30Z", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := makeGhResponse("OPEN",
				[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS",
					StartedAt: tt.startAt, CompletedAt: tt.endAt}},
				nil, "MERGEABLE",
			)
			createMockGh(t, resp)

			storePath := filepath.Join(t.TempDir(), "prs.json")
			p := NewPoller(storePath, nil)
			p.Add("test", "repo", 1)
			p.pollOne("test", "repo", 1)

			p.mu.RLock()
			pr := p.tracked["test/repo#1"]
			p.mu.RUnlock()

			if pr.Checks[0].Duration == "" {
				t.Fatal("duration should be set")
			}
			if tt.wantShort {
				if pr.Checks[0].Duration != "30s" {
					t.Errorf("short duration = %q, want 30s", pr.Checks[0].Duration)
				}
			} else {
				if pr.Checks[0].Duration != "5m 30s" {
					t.Errorf("long duration = %q, want '5m 30s'", pr.Checks[0].Duration)
				}
			}
		})
	}
}

func TestPollOne_StateTransitionTimeline(t *testing.T) {
	// First poll: passing.
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	// Disable auto-merge and hammer to avoid extra timeline events.
	tracked.AutopilotMode = PROff
	tracked.Hammer = false

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	initialTimeline := len(p.tracked["test/repo#1"].Timeline)
	p.mu.RUnlock()

	// Second poll: same state → no new timeline event.
	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	afterSame := len(p.tracked["test/repo#1"].Timeline)
	p.mu.RUnlock()

	if afterSame != initialTimeline {
		t.Errorf("same state should not add timeline event: %d → %d", initialTimeline, afterSame)
	}

	// Third poll: state changes to failing.
	resp = makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	afterChange := len(p.tracked["test/repo#1"].Timeline)
	p.mu.RUnlock()

	if afterChange != afterSame+1 {
		t.Errorf("state change should add 1 timeline event: %d → %d", afterSame, afterChange)
	}
}

func TestPollOne_RemovedDuringPoll(t *testing.T) {
	resp := makeGhResponse("OPEN", nil, nil, "MERGEABLE")
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	// Remove the PR.
	p.Remove("test", "repo", 1)

	// pollOne should handle gracefully.
	changed := p.pollOne("test", "repo", 1)
	if changed {
		t.Error("removed PR should return false")
	}
}

func TestPollOne_GhCommandFails(t *testing.T) {
	// Create a gh script that exits with error.
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	script := "#!/bin/sh\nexit 1\n"
	os.WriteFile(ghPath, []byte(script), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	changed := p.pollOne("test", "repo", 1)
	if changed {
		t.Error("failed gh command should return false")
	}
}

func TestPollOne_GhReturnsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	script := "#!/bin/sh\necho 'not valid json'\n"
	os.WriteFile(ghPath, []byte(script), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	changed := p.pollOne("test", "repo", 1)
	if changed {
		t.Error("invalid JSON should return false")
	}
}

func TestPollOne_DraftPR(t *testing.T) {
	data := ghPRData{
		Title:       "Draft PR",
		HeadRefName: "feat",
		BaseRefName: "main",
		URL:         "https://github.com/test/repo/pull/1",
		State:       "OPEN",
		Mergeable:   "MERGEABLE",
		IsDraft:     true,
		Commits:     []ghCommit{{}},
	}
	b, _ := json.Marshal(data)
	createMockGh(t, string(b))

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if !pr.IsDraft {
		t.Error("isDraft should be true")
	}
}

func TestPollOne_QueuedChecks(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "QUEUED", Conclusion: ""}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.State != StateChecksRunning {
		t.Errorf("QUEUED check: state = %q, want checks_running", pr.State)
	}
}

// --- Poll ---

func TestPoll_MultiplePRs(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	changed := false
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, func() { changed = true })
	pr1, _ := p.Add("test", "repo", 1)
	pr2, _ := p.Add("test", "repo", 2)
	// Disable review spawning — this test is about polling, not agent execution.
	pr1.ReviewState = "clean"
	pr2.ReviewState = "clean"

	p.Poll()

	if !changed {
		t.Error("Poll should trigger onChange")
	}
}

func TestPoll_Empty(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	// Should not panic on empty tracker.
	p.Poll()
}

// --- RunLoop ---

func TestRunLoop_StopsOnSignal(t *testing.T) {
	resp := makeGhResponse("OPEN", nil, nil, "MERGEABLE")
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		RunLoop(p, 50*time.Millisecond, stop)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not stop")
	}
}

// --- triggerMerge ---

func TestTriggerMerge_Squash(t *testing.T) {
	// Mock gh that records the command — we just check it doesn't crash.
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	script := "#!/bin/sh\nexit 0\n"
	os.WriteFile(ghPath, []byte(script), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	changed := false
	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, func() { changed = true })
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "squash"
	changed = false // Reset after Add.

	p.triggerMerge(tracked)

	if !changed {
		t.Error("triggerMerge should call onChange")
	}

	p.mu.RLock()
	timeline := p.tracked["test/repo#1"].Timeline
	p.mu.RUnlock()

	lastEvent := timeline[len(timeline)-1]
	if lastEvent.Message != "Auto-merge triggered (squash)" {
		t.Errorf("timeline message = %q", lastEvent.Message)
	}
}

func TestTriggerMerge_Rebase(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	os.WriteFile(ghPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "rebase"

	p.triggerMerge(tracked)

	p.mu.RLock()
	timeline := p.tracked["test/repo#1"].Timeline
	p.mu.RUnlock()

	lastEvent := timeline[len(timeline)-1]
	if lastEvent.Message != "Auto-merge triggered (rebase)" {
		t.Errorf("timeline message = %q", lastEvent.Message)
	}
}

func TestTriggerMerge_Merge(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	os.WriteFile(ghPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "merge"

	p.triggerMerge(tracked)

	p.mu.RLock()
	timeline := p.tracked["test/repo#1"].Timeline
	p.mu.RUnlock()

	lastEvent := timeline[len(timeline)-1]
	if lastEvent.Message != "Auto-merge triggered (merge)" {
		t.Errorf("timeline message = %q", lastEvent.Message)
	}
}

func TestTriggerMerge_Aviator(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	os.WriteFile(ghPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "aviator"

	p.triggerMerge(tracked)

	p.mu.RLock()
	timeline := p.tracked["test/repo#1"].Timeline
	p.mu.RUnlock()

	lastEvent := timeline[len(timeline)-1]
	if lastEvent.Message != "Auto-merge triggered (aviator)" {
		t.Errorf("timeline message = %q", lastEvent.Message)
	}
}

func TestTriggerMerge_Failure(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	os.WriteFile(ghPath, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "squash"

	p.triggerMerge(tracked)

	p.mu.RLock()
	timeline := p.tracked["test/repo#1"].Timeline
	p.mu.RUnlock()

	lastEvent := timeline[len(timeline)-1]
	if lastEvent.Icon != "✗" {
		t.Errorf("failure icon = %q, want '✗'", lastEvent.Icon)
	}
}

func TestTriggerMerge_RemovedDuringMerge(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	os.WriteFile(ghPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	old := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = old })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.MergeMethod = "squash"

	// Remove before triggerMerge runs its lock section.
	p.Remove("test", "repo", 1)

	// Should not panic.
	p.triggerMerge(tracked)
}

// --- Hammer logic in pollOne ---

func TestPollOne_HammerOnChecksFailing(t *testing.T) {
	// First poll: passing.
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.Hammer = true
	tracked.AutopilotMode = PRAuto
	tracked.MaxHammer = 3
	tracked.ReviewState = "clean" // skip review — this test is about hammering

	p.pollOne("test", "repo", 1)

	// Now fail the checks.
	resp = makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	if pr.HammerCount != 1 {
		t.Errorf("hammerCount = %d, want 1", pr.HammerCount)
	}
	if pr.AgentRunning != "fix_ci" {
		t.Errorf("AgentRunning = %q, want fix_ci", pr.AgentRunning)
	}
}

func TestPollOne_NoHammerOnSameState(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.Hammer = true
	tracked.AutopilotMode = PRAuto
	tracked.MaxHammer = 3

	p.pollOne("test", "repo", 1)
	// Poll again with same failing state.
	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	// Hammer should only fire on state CHANGE.
	if pr.HammerCount != 1 {
		t.Errorf("hammerCount = %d, want 1 (no re-hammer on same state)", pr.HammerCount)
	}
}

// --- ghBin fallback ---

func TestGhBin_Fallback(t *testing.T) {
	// With PATH cleared, ghBin should still return something.
	// We can't fully test this without modifying the filesystem,
	// but we can verify it doesn't panic.
	bin := ghBin()
	if bin == "" {
		t.Error("ghBin should never return empty")
	}
}

// --- Auto-merge triggered by pollOne ---

func TestPollOne_AutoMergeTrigger(t *testing.T) {
	// Use a mock gh that succeeds for both view and merge.
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		[]ghReview{{Author: ghAuthor{Login: "rev"}, State: "APPROVED", SubmittedAt: time.Now().Add(-1 * time.Hour)}},
		"MERGEABLE",
	)
	// gh script: if "view" in args, return PR data; otherwise exit 0.
	script := fmt.Sprintf(`#!/bin/sh
case "$2" in
  view) echo '%s' ;;
  *) exit 0 ;;
esac
`, resp)
	os.WriteFile(ghPath, []byte(script), 0o755)
	oldFn := ghBinFunc
	ghBinFunc = func() string { return ghPath }
	t.Cleanup(func() { ghBinFunc = oldFn })

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	tracked, _ := p.Add("test", "repo", 1)
	tracked.AutopilotMode = PRAuto
	tracked.MergeMethod = "squash"

	p.pollOne("test", "repo", 1)

	// Wait for the auto-merge goroutine.
	time.Sleep(200 * time.Millisecond)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	// Timeline should have an auto-merge event.
	found := false
	for _, ev := range pr.Timeline {
		if ev.Message == "Auto-merge triggered (squash)" {
			found = true
		}
	}
	if !found {
		t.Error("expected auto-merge timeline event")
	}
}

// --- Check with no duration ---

func TestPollOne_CheckNoDuration(t *testing.T) {
	resp := makeGhResponse("OPEN",
		[]ghCheck{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}},
		nil, "MERGEABLE",
	)
	createMockGh(t, resp)

	storePath := filepath.Join(t.TempDir(), "prs.json")
	p := NewPoller(storePath, nil)
	p.Add("test", "repo", 1)
	p.pollOne("test", "repo", 1)

	p.mu.RLock()
	pr := p.tracked["test/repo#1"]
	p.mu.RUnlock()

	// No startedAt/completedAt → no duration.
	if pr.Checks[0].Duration != "" {
		t.Errorf("duration = %q, want empty", pr.Checks[0].Duration)
	}
}
