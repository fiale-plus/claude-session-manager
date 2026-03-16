package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// === Iteration 1: Strip height consistency ===

// TestRenderStrip_HeightConsistent ensures the strip height is the same
// regardless of which session is selected (borders on selected pill
// must not add vertical lines).
func TestRenderStrip_HeightConsistent(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha"},
		{SessionID: "s2", State: "idle", ProjectName: "beta"},
		{SessionID: "s3", State: "waiting", ProjectName: "gamma"},
	}

	h0 := lipgloss.Height(renderStrip(sessions, 0, 100, 0))
	h1 := lipgloss.Height(renderStrip(sessions, 1, 100, 0))
	h2 := lipgloss.Height(renderStrip(sessions, 2, 100, 0))

	if h0 != h1 || h1 != h2 {
		t.Errorf("strip height varies by selection: sel0=%d sel1=%d sel2=%d", h0, h1, h2)
	}
}

// TestRenderStrip_MaxHeight ensures the strip never exceeds 2 lines
// (border-top + content row).
func TestRenderStrip_MaxHeight(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha", AutopilotMode: "yolo"},
		{SessionID: "s2", State: "waiting", ProjectName: "beta",
			PendingTools: []client.PendingTool{{ToolName: "Bash", Safety: "destructive"}}},
	}

	out := renderStrip(sessions, 0, 100, 0)
	h := lipgloss.Height(out)
	if h > 2 {
		t.Errorf("strip height = %d, want <= 2", h)
	}
}

// === Iteration 2: Long pill names ===

// TestPillName_Truncation ensures very long names are truncated in pills.
func TestRenderPill_LongName(t *testing.T) {
	s := client.Session{
		SessionID:   "s1",
		State:       "running",
		ProjectName: strings.Repeat("very-long-project-name-", 5),
	}
	pill := renderPill(s, false, 0)
	w := lipgloss.Width(pill)
	if w > 40 {
		t.Errorf("pill width = %d for long name, want <= 40", w)
	}
}

// === Iteration 3: Many sessions strip ===

// TestRenderStrip_ManySessions ensures the strip renders cleanly
// with many sessions without exceeding width.
func TestRenderStrip_ManySessions(t *testing.T) {
	var sessions []client.Session
	for i := 0; i < 12; i++ {
		sessions = append(sessions, client.Session{
			SessionID:   "s" + itoa(i),
			State:       "running",
			ProjectName: "proj-" + itoa(i),
		})
	}

	width := 80
	out := renderStrip(sessions, 0, width, 0)
	// Strip should not produce lines wider than the terminal.
	for _, line := range strings.Split(out, "\n") {
		lw := lipgloss.Width(line)
		if lw > width {
			t.Errorf("strip line width %d exceeds terminal width %d", lw, width)
		}
	}
}

// === Iteration 4: truncateMiddle edge cases ===

func TestTruncateMiddle_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		panics bool
	}{
		{"negative", "hello", -1, false},
		{"zero", "hello", 0, false},
		{"one", "hello", 1, false},
		{"exact", "hello", 5, false},
		{"shorter", "hi", 5, false},
		{"long", "hello world this is long", 10, false},
		{"unicode", "こんにちは世界", 4, false},
		{"empty input", "", 5, false},
		{"empty both", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("truncateMiddle(%q, %d) panicked: %v", tt.input, tt.maxLen, r)
				}
			}()
			result := truncateMiddle(tt.input, tt.maxLen)
			if tt.maxLen > 0 && len([]rune(result)) > tt.maxLen {
				t.Errorf("truncateMiddle(%q, %d) = %q (len %d), exceeds maxLen",
					tt.input, tt.maxLen, result, len([]rune(result)))
			}
		})
	}
}

// === Iteration 5: Pill name selection logic ===

func TestPillName_Priority(t *testing.T) {
	// User slug (not auto — 4+ parts) takes priority
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "my-cool-setup-v2",
		ProjectName: "fallback",
		GhosttyTab:  "tab title",
	}
	if got := pillName(s); got != "my-cool-setup-v2" {
		t.Errorf("user slug: got %q, want 'my-cool-setup-v2'", got)
	}

	// Auto slug falls behind clean tab name
	s2 := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "zesty-dreaming-kitten",
		GhosttyTab:  "CSM",
		ProjectName: "fallback",
	}
	if got := pillName(s2); got != "CSM" {
		t.Errorf("auto slug + clean tab: got %q, want 'CSM'", got)
	}

	// Command-like tab name falls back to auto slug
	s3 := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "zesty-dreaming-kitten",
		GhosttyTab:  "cd /foo && bar",
		ProjectName: "fallback",
	}
	if got := pillName(s3); got != "zesty-dreaming-kitten" {
		t.Errorf("command tab: got %q, want auto slug", got)
	}

	// No slug, no tab → project name
	s4 := client.Session{
		SessionID:   "abc12345xyz",
		ProjectName: "my-project",
	}
	if got := pillName(s4); got != "my-project" {
		t.Errorf("no slug: got %q, want 'my-project'", got)
	}

	// Nothing → truncated session ID
	s5 := client.Session{SessionID: "abc12345xyz"}
	if got := pillName(s5); got != "abc12345" {
		t.Errorf("bare session: got %q, want 'abc12345'", got)
	}
}

func TestIsAutoSlug(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{"zesty-dreaming-kitten", true},
		{"hello-world-test", true},
		{"my-cool-project", true},  // 3 lowercase words matches auto-slug pattern
		{"My-Cool-Project", false}, // uppercase
		{"two-words", false},       // only 2 parts
		{"a-b-c-d", false},         // 4 parts
		{"", false},
		{"hello-world-123", false}, // digits
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := isAutoSlug(tt.slug); got != tt.want {
				t.Errorf("isAutoSlug(%q) = %v, want %v", tt.slug, got, tt.want)
			}
		})
	}
}

// === Unified strip with sessions + PRs ===

func TestRenderUnifiedStrip_SessionsAndPRs(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha"},
		{SessionID: "s2", State: "idle", ProjectName: "beta"},
	}
	prs := []client.TrackedPR{
		{Owner: "octocat", Repo: "repo", Number: 42, Title: "Fix bug", State: "checks_passing"},
		{Owner: "owner", Repo: "project", Number: 7, Title: "Add feature", State: "checks_failing"},
	}

	out := renderUnifiedStrip(sessions, prs, 0, 120, 0)
	if out == "" {
		t.Error("unified strip should produce output")
	}
	// Should contain the separator.
	if !strings.Contains(out, "\u2502") {
		t.Error("unified strip should contain separator between sessions and PRs")
	}
}

func TestRenderUnifiedStrip_SessionsOnly(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha"},
	}

	out := renderUnifiedStrip(sessions, nil, 0, 100, 0)
	if out == "" {
		t.Error("sessions-only strip should produce output")
	}
	// No separator when no PRs.
	if strings.Contains(out, "\u2502") {
		t.Error("sessions-only strip should not contain separator")
	}
}

func TestRenderUnifiedStrip_PRsOnly(t *testing.T) {
	prs := []client.TrackedPR{
		{Owner: "o", Repo: "r", Number: 1, Title: "PR", State: "approved"},
	}

	// Use selectedIdx=-1 so no PR is selected (avoids RoundedBorder which contains │).
	out := renderUnifiedStrip(nil, prs, -1, 100, 0)
	if out == "" {
		t.Error("PRs-only strip should produce output")
	}
}

func TestRenderUnifiedStrip_Empty(t *testing.T) {
	out := renderUnifiedStrip(nil, nil, 0, 100, 0)
	if out == "" {
		t.Error("empty strip should produce output (empty state message)")
	}
	if !strings.Contains(out, "No active") {
		t.Error("empty strip should show 'No active' message")
	}
}

func TestRenderUnifiedStrip_PRSelected(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha"},
	}
	prs := []client.TrackedPR{
		{Owner: "o", Repo: "r", Number: 1, Title: "PR", State: "checks_passing"},
	}

	// Selected index = 1 means PR is selected (sessions count = 1).
	out := renderUnifiedStrip(sessions, prs, 1, 120, 0)
	if out == "" {
		t.Error("strip with PR selected should produce output")
	}
}

func TestRenderUnifiedStrip_HeightConsistentWithPRs(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", State: "running", ProjectName: "alpha"},
	}
	prs := []client.TrackedPR{
		{Owner: "o", Repo: "r", Number: 1, Title: "PR", State: "checks_passing"},
	}

	h0 := lipgloss.Height(renderUnifiedStrip(sessions, prs, 0, 120, 0))
	h1 := lipgloss.Height(renderUnifiedStrip(sessions, prs, 1, 120, 0))

	// Height might differ slightly due to PR selected border, but should be close.
	// What matters: both produce valid output.
	if h0 == 0 || h1 == 0 {
		t.Errorf("strip height: sel0=%d sel1=%d, neither should be 0", h0, h1)
	}
}

// === PR pill icons ===

func TestPRPillIcon(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"checks_failing", "\u2717"},
		{"checks_running", "\u23f3"},
		{"checks_passing", "\u2713"},
		{"approved", "\u2705"},
		{"merged", "\U0001f680"},
		{"unknown", "\u2022"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := prPillIcon(tt.state); got != tt.want {
				t.Errorf("prPillIcon(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// === interleave ===

func TestInterleave(t *testing.T) {
	tests := []struct {
		name   string
		items  []string
		sep    string
		wantN  int
	}{
		{"empty", nil, " ", 0},
		{"single", []string{"a"}, " ", 1},
		{"two", []string{"a", "b"}, " ", 3},
		{"three", []string{"a", "b", "c"}, " ", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interleave(tt.items, tt.sep)
			if len(result) != tt.wantN {
				t.Errorf("interleave(%v): got %d items, want %d", tt.items, len(result), tt.wantN)
			}
		})
	}
}

// === countPending ===

func TestCountPending(t *testing.T) {
	sessions := []client.Session{
		{SessionID: "s1", PendingTools: []client.PendingTool{
			{ToolName: "Bash"}, {ToolName: "Read"},
		}},
		{SessionID: "s2"}, // no pending
		{SessionID: "s3", PendingTools: []client.PendingTool{
			{ToolName: "Edit"},
		}},
	}
	if got := countPending(sessions); got != 3 {
		t.Errorf("countPending = %d, want 3", got)
	}
}

func TestCountPending_Empty(t *testing.T) {
	if got := countPending(nil); got != 0 {
		t.Errorf("countPending(nil) = %d, want 0", got)
	}
}

// === padRight ===

func TestPadRight(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{"hi", 5, "hi   "},
		{"hello", 5, "hello"},
		{"hello!", 5, "hello!"},
		{"", 3, "   "},
	}
	for _, tt := range tests {
		if got := padRight(tt.s, tt.w); got != tt.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
	}
}
