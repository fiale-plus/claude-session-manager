package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

func testPR() client.TrackedPR {
	return client.TrackedPR{
		Owner:       "octocat",
		Repo:        "hello-world",
		Number:      42,
		Title:       "Fix the thing",
		HeadBranch:  "fix/thing",
		BaseBranch:  "main",
		URL:         "https://github.com/octocat/hello-world/pull/42",
		State:       "checks_passing",
		Mergeable:   "MERGEABLE",
		Additions:   25,
		Deletions:   10,
		CommitCount: 3,
		AutopilotMode: "auto",
		Hammer:      true,
		Checks: []client.PRCheck{
			{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS", Duration: "2m 30s"},
			{Name: "lint", Status: "COMPLETED", Conclusion: "SUCCESS", Duration: "45s"},
		},
		Reviews: []client.PRReview{
			{Author: "reviewer", State: "APPROVED", Body: "LGTM", At: "5m ago"},
		},
		Timeline: []client.PREvent{
			{Time: time.Now().Add(-10 * time.Minute), Icon: "p", Message: "Added to tracking"},
			{Time: time.Now().Add(-5 * time.Minute), Icon: "v", Message: "State: -> checks_passing"},
		},
	}
}

// === Height compliance ===

func TestRenderPRZoom_NeverExceedsHeight(t *testing.T) {
	pr := testPR()

	dims := []struct {
		name   string
		width  int
		height int
	}{
		{"normal", 120, 20},
		{"short", 120, 8},
		{"narrow", 40, 20},
		{"narrow+short", 40, 8},
		{"very narrow", 25, 15},
	}

	for _, tt := range dims {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPRZoom(pr, tt.width, tt.height, 0)
			lines := strings.Split(out, "\n")
			if len(lines) > tt.height {
				t.Errorf("renderPRZoom produced %d lines, want <= %d", len(lines), tt.height)
			}
		})
	}
}

func TestRenderPRZoom_MinimumSize(t *testing.T) {
	pr := testPR()

	// Below minimum should return empty.
	out := renderPRZoom(pr, 5, 3, 0)
	if out != "" {
		t.Error("width=5 height=3 should produce empty output")
	}

	// At minimum should produce something.
	out = renderPRZoom(pr, 10, 4, 0)
	if out == "" {
		t.Error("width=10 height=4 should produce output")
	}
}

// === State labels and colors ===

func TestPRStateLabel(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"checks_failing", "FAILING"},
		{"checks_running", "RUNNING"},
		{"checks_passing", "PASSING"},
		{"approved", "APPROVED"},
		{"merged", "MERGED"},
		{"closed", "CLOSED"},
		{"unknown_state", "UNKNOWN_STATE"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := prStateLabel(tt.state); got != tt.want {
				t.Errorf("prStateLabel(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestPRStateColor_AllStates(t *testing.T) {
	// Should not panic for any known or unknown state.
	states := []string{"checks_failing", "checks_running", "checks_passing", "approved", "merged", "closed", "unknown"}
	for _, s := range states {
		c := prStateColor(s)
		if c == nil {
			t.Errorf("prStateColor(%q) returned nil", s)
		}
	}
}

func TestPRStateColor_MergedIsDim(t *testing.T) {
	// Merged PRs should use a dim color, not magenta.
	mergedColor := prStateColor("merged")
	if mergedColor == lipgloss.ANSIColor(5) { // magenta
		t.Error("merged state should not use magenta — use dim color instead")
	}
	// Should be the dim foreground color.
	if mergedColor != colorDimFg {
		t.Errorf("merged state color should be colorDimFg, got %v", mergedColor)
	}
}

// === Check icons and status text ===

func TestCheckIcon(t *testing.T) {
	tests := []struct {
		check client.PRCheck
		name  string
	}{
		{client.PRCheck{Conclusion: "SUCCESS"}, "success"},
		{client.PRCheck{Conclusion: "FAILURE"}, "failure"},
		{client.PRCheck{Conclusion: "NEUTRAL"}, "neutral"},
		{client.PRCheck{Status: "IN_PROGRESS"}, "in-progress"},
		{client.PRCheck{Status: "QUEUED"}, "queued"},
		{client.PRCheck{}, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic.
			got := checkIcon(tt.check)
			if got == "" {
				t.Error("checkIcon should return non-empty string")
			}
		})
	}
}

func TestCheckStatusText(t *testing.T) {
	tests := []struct {
		check client.PRCheck
		name  string
		want  string // substring to verify
	}{
		{client.PRCheck{Conclusion: "SUCCESS"}, "success", "passed"},
		{client.PRCheck{Conclusion: "FAILURE"}, "failure", "failed"},
		{client.PRCheck{Conclusion: "NEUTRAL"}, "neutral", "neutral"},
		{client.PRCheck{Status: "IN_PROGRESS"}, "running", "running"},
		{client.PRCheck{Status: "QUEUED"}, "queued", "queued"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkStatusText(tt.check)
			if !strings.Contains(got, tt.want) {
				t.Errorf("checkStatusText missing %q, got %q", tt.want, got)
			}
		})
	}
}

// === Review icons ===

func TestReviewIcon(t *testing.T) {
	states := []string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED", "PENDING", ""}
	for _, s := range states {
		t.Run(s, func(t *testing.T) {
			got := reviewIcon(s)
			if got == "" {
				t.Error("reviewIcon should return non-empty string")
			}
		})
	}
}

// === Hyperlink ===

func TestHyperlink(t *testing.T) {
	result := hyperlink("https://example.com", "Click me")
	if !strings.Contains(result, "https://example.com") {
		t.Error("hyperlink should contain URL")
	}
	if !strings.Contains(result, "Click me") {
		t.Error("hyperlink should contain text")
	}
	// Should contain OSC 8 escape sequences.
	if !strings.Contains(result, "\033]8;;") {
		t.Error("hyperlink should contain OSC 8 escape")
	}
}

// === Rendering with various PR states ===

func TestRenderPRZoom_FailingChecks(t *testing.T) {
	pr := testPR()
	pr.State = "checks_failing"
	pr.Checks = []client.PRCheck{
		{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE", Detail: "Exit code 1"},
		{Name: "lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	out := renderPRZoom(pr, 100, 20, 0)
	if out == "" {
		t.Error("should render failing PR")
	}
	if !strings.Contains(out, "FAILING") {
		t.Error("should show FAILING state label")
	}
}

func TestRenderPRZoom_RunningChecks(t *testing.T) {
	pr := testPR()
	pr.State = "checks_running"
	pr.Checks = []client.PRCheck{
		{Name: "ci", Status: "IN_PROGRESS", Conclusion: ""},
		{Name: "lint", Status: "QUEUED", Conclusion: ""},
	}

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "RUNNING") {
		t.Error("should show RUNNING state label")
	}
}

func TestRenderPRZoom_MergedState(t *testing.T) {
	pr := testPR()
	pr.State = "merged"
	pr.Checks = nil
	pr.Reviews = nil

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "MERGED") {
		t.Error("should show MERGED state label")
	}
}

func TestRenderPRZoom_Conflicting(t *testing.T) {
	pr := testPR()
	pr.Mergeable = "CONFLICTING"

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "conflict") {
		t.Error("should show 'conflicts' for conflicting PR")
	}
}

func TestRenderPRZoom_AutopilotAuto(t *testing.T) {
	pr := testPR()
	pr.AutopilotMode = "auto"

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "AUTO") {
		t.Error("auto mode should show AUTO badge")
	}
}

func TestRenderPRZoom_AutopilotYolo(t *testing.T) {
	pr := testPR()
	pr.AutopilotMode = "yolo"

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "YOLO") {
		t.Error("yolo mode should show YOLO badge")
	}
}

func TestRenderPRZoom_AutopilotOff(t *testing.T) {
	pr := testPR()
	pr.AutopilotMode = "off"

	out := renderPRZoom(pr, 100, 20, 0)
	if strings.Contains(out, "AUTO") || strings.Contains(out, "YOLO") {
		t.Error("off mode should not show AUTO or YOLO badge")
	}
}

func TestRenderPRZoom_NoChecks(t *testing.T) {
	pr := testPR()
	pr.Checks = nil

	out := renderPRZoom(pr, 100, 20, 0)
	if out == "" {
		t.Error("PR with no checks should still render")
	}
	// Should not contain "Checks" section header.
	if strings.Contains(out, "Checks (") {
		t.Error("no checks: should not show Checks section header")
	}
}

func TestRenderPRZoom_NoReviews(t *testing.T) {
	pr := testPR()
	pr.Reviews = nil

	out := renderPRZoom(pr, 100, 20, 0)
	if out == "" {
		t.Error("PR with no reviews should still render")
	}
	if strings.Contains(out, "Reviews") {
		t.Error("no reviews: should not show Reviews section header")
	}
}

func TestRenderPRZoom_NoTimeline(t *testing.T) {
	pr := testPR()
	pr.Timeline = nil

	out := renderPRZoom(pr, 100, 20, 0)
	if out == "" {
		t.Error("PR with no timeline should still render")
	}
	if strings.Contains(out, "Timeline") {
		t.Error("no timeline: should not show Timeline section header")
	}
}

func TestRenderPRZoom_HammerBadge(t *testing.T) {
	pr := testPR()
	pr.Hammer = true

	out := renderPRZoom(pr, 100, 20, 0)
	// Hammer emoji should appear.
	if !strings.Contains(out, "\U0001f528") {
		t.Error("hammer=true should show hammer emoji")
	}
}

func TestRenderPRZoom_NoHammerBadge(t *testing.T) {
	pr := testPR()
	pr.Hammer = false

	out := renderPRZoom(pr, 100, 20, 0)
	if strings.Contains(out, "\U0001f528") {
		t.Error("hammer=false should not show hammer emoji")
	}
}

// === Scroll clamping ===

func TestRenderPRZoom_ScrollClamp(t *testing.T) {
	pr := testPR()
	// Huge scroll offset should not panic.
	out := renderPRZoom(pr, 80, 20, 9999)
	if out == "" {
		t.Error("huge scroll offset should produce output")
	}
}

func TestRenderPRZoom_ScrollZero(t *testing.T) {
	pr := testPR()
	out := renderPRZoom(pr, 80, 20, 0)
	if out == "" {
		t.Error("scroll=0 should produce output")
	}
}

// === PR info line ===

func TestRenderPRZoom_ShowsBranchInfo(t *testing.T) {
	pr := testPR()
	out := renderPRZoom(pr, 120, 20, 0)
	if !strings.Contains(out, "fix/thing") {
		t.Error("should show head branch")
	}
	if !strings.Contains(out, "main") {
		t.Error("should show base branch")
	}
}

func TestRenderPRZoom_ShowsAdditionsDeletions(t *testing.T) {
	pr := testPR()
	out := renderPRZoom(pr, 120, 20, 0)
	if !strings.Contains(out, "+25") {
		t.Error("should show additions")
	}
	if !strings.Contains(out, "-10") {
		t.Error("should show deletions")
	}
}

func TestRenderPRZoom_ShowsCommitCount(t *testing.T) {
	pr := testPR()
	out := renderPRZoom(pr, 120, 20, 0)
	if !strings.Contains(out, "3 commits") {
		t.Error("should show commit count")
	}
}

func TestRenderPRZoom_ContainsURL(t *testing.T) {
	pr := testPR()
	out := renderPRZoom(pr, 120, 30, 0)
	if !strings.Contains(out, "https://github.com/octocat/hello-world/pull/42") {
		t.Error("should contain PR URL")
	}
}

// === Width compliance ===

func TestRenderPRZoom_WidthCompliance(t *testing.T) {
	pr := testPR()
	width := 80
	out := renderPRZoom(pr, width, 20, 0)
	for _, line := range strings.Split(out, "\n") {
		w := lipgloss.Width(line)
		if w > width {
			t.Errorf("line width %d exceeds terminal width %d", w, width)
		}
	}
}

// === Review with body ===

func TestRenderPRZoom_ReviewWithBody(t *testing.T) {
	pr := testPR()
	pr.Reviews = []client.PRReview{
		{Author: "bob", State: "APPROVED", Body: "Looks great!", At: "2m ago"},
	}

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "@bob") {
		t.Error("should show reviewer name")
	}
}

func TestRenderPRZoom_ChangesRequestedReview(t *testing.T) {
	pr := testPR()
	pr.State = "checks_passing"
	pr.Reviews = []client.PRReview{
		{Author: "alice", State: "CHANGES_REQUESTED", Body: "Needs work"},
	}

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "@alice") {
		t.Error("should show reviewer name")
	}
}

// === Check with duration ===

func TestRenderPRZoom_CheckWithDuration(t *testing.T) {
	pr := testPR()
	pr.Checks = []client.PRCheck{
		{Name: "ci", Conclusion: "SUCCESS", Duration: "5m 30s"},
	}

	out := renderPRZoom(pr, 100, 20, 0)
	if !strings.Contains(out, "5m 30s") {
		t.Error("should show check duration")
	}
}

// === Empty PR (minimal data) ===

func TestRenderPRZoom_MinimalPR(t *testing.T) {
	pr := client.TrackedPR{
		Owner:  "a",
		Repo:   "b",
		Number: 1,
	}
	// Should not panic with minimal data.
	out := renderPRZoom(pr, 80, 20, 0)
	if out == "" {
		t.Error("minimal PR should still render")
	}
}
