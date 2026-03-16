package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

func testSession() client.Session {
	now := time.Now()
	return client.Session{
		SessionID: "test-1234-abcd",
		CWD:       "/Users/test/repos/my-project",
		State:     "running",
		PID:       12345,
		GitBranch: "main",
		LastText:  "Working on something interesting",
		Activities: []client.Activity{
			{Timestamp: now.Add(-3 * time.Minute), ActivityType: "tool_use", Summary: "Edit: foo.go"},
			{Timestamp: now.Add(-2 * time.Minute), ActivityType: "tool_use", Summary: "Read: bar.go"},
			{Timestamp: now.Add(-1 * time.Minute), ActivityType: "text", Summary: "Done editing"},
		},
		LastActivity: &now,
	}
}

// TestRenderZoom_NeverExceedsHeight verifies the zoom panel output never
// exceeds its allocated height — the bug that pushed the strip off-screen.
func TestRenderZoom_NeverExceedsHeight(t *testing.T) {
	s := testSession()

	for _, tt := range []struct {
		name   string
		width  int
		height int
	}{
		{"normal", 120, 20},
		{"short", 120, 8},
		{"narrow", 40, 20},
		{"narrow+short", 40, 8},
		{"very narrow wraps lines", 25, 15},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := renderZoom(s, tt.width, tt.height, 0)
			lines := strings.Split(out, "\n")
			if len(lines) > tt.height {
				t.Errorf("renderZoom produced %d lines, want <= %d", len(lines), tt.height)
			}
		})
	}
}

// TestRenderZoom_LongContent_ClipsCleanly ensures that sessions with long
// CWD, branch names, or last text don't overflow height.
func TestRenderZoom_LongContent_ClipsCleanly(t *testing.T) {
	s := testSession()
	s.CWD = "/very/long/path/" + strings.Repeat("subdir/", 20) + "project"
	s.GitBranch = "feature/" + strings.Repeat("long-branch-name-", 5)
	s.LastText = strings.Repeat("This is a very long output message. ", 10)
	s.AutopilotMode = "yolo"

	width, height := 80, 15
	out := renderZoom(s, width, height, 0)
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Errorf("renderZoom with long content produced %d lines, want <= %d", len(lines), height)
	}
}

// TestRenderZoom_WithPendingTools ensures pending tools section doesn't overflow.
func TestRenderZoom_WithPendingTools(t *testing.T) {
	s := testSession()
	s.PendingTools = []client.PendingTool{
		{ToolName: "Bash", ToolInput: map[string]any{"command": "rm -rf /"}, Safety: "destructive"},
		{ToolName: "Edit", ToolInput: map[string]any{"file_path": "/some/file.go"}, Safety: "safe"},
	}

	width, height := 80, 12
	out := renderZoom(s, width, height, 0)
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Errorf("renderZoom with pending tools produced %d lines, want <= %d", len(lines), height)
	}
}

// === Iteration 6: Human-readable relative time ===

func TestFormatAge(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{3 * time.Second, "3s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{65 * time.Minute, "1h 5m"},
		{2 * time.Hour, "2h"},
		{26 * time.Hour, "1d 2h"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatAge(tt.dur)
			if got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

// === Iteration 7: Activity overflow indicator ===

func TestRenderZoom_ManyActivities_ShowsOverflow(t *testing.T) {
	s := testSession()
	now := time.Now()
	// Add 15 activities — only 8 should be shown, with overflow indicator.
	s.Activities = nil
	for i := 0; i < 15; i++ {
		s.Activities = append(s.Activities, client.Activity{
			Timestamp:    now.Add(-time.Duration(15-i) * time.Minute),
			ActivityType: "tool_use",
			Summary:      "Action " + itoa(i),
		})
	}

	out := renderZoom(s, 100, 30, 0)
	// Should indicate there are older activities not shown.
	if !strings.Contains(out, "+") || !strings.Contains(out, "more") {
		t.Error("15 activities should show overflow indicator like '+7 more'")
	}
}

// === Iteration 8: Zoom at minimum height ===

func TestRenderZoom_MinimumHeight(t *testing.T) {
	s := testSession()
	// Height=4 is the minimum — should not panic or produce empty output.
	out := renderZoom(s, 80, 4, 0)
	if out == "" {
		t.Error("renderZoom at height=4 should produce output")
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 4 {
		t.Errorf("renderZoom at height=4 produced %d lines", len(lines))
	}
}

// === Iteration 9: Scroll clamping ===

func TestRenderZoom_ScrollClamp(t *testing.T) {
	s := testSession()
	// Scroll offset way beyond content should not panic.
	out := renderZoom(s, 80, 20, 9999)
	if out == "" {
		t.Error("renderZoom with huge scroll offset should produce output")
	}
}

// === Iteration 10: Permission mode display ===

func TestRenderZoom_ShowsPermissionMode(t *testing.T) {
	s := testSession()
	s.PermissionMode = "plan"
	out := renderZoom(s, 100, 20, 0)
	if !strings.Contains(out, "PLAN") {
		t.Error("session with permission_mode=plan should show PLAN badge")
	}
}

// TestFullView_NeverExceedsTerminalHeight is the integration-level regression:
// the full View() output must never exceed m.height.
// === Iteration 10a: Tool detail rendering ===

func TestToolDetail_AllKeys(t *testing.T) {
	tests := []struct {
		name   string
		tool   client.PendingTool
		expect string
	}{
		{"command", client.PendingTool{ToolInput: map[string]any{"command": "ls -la"}}, "ls -la"},
		{"file_path", client.PendingTool{ToolInput: map[string]any{"file_path": "/foo.go"}}, "/foo.go"},
		{"pattern", client.PendingTool{ToolInput: map[string]any{"pattern": "TODO"}}, "TODO"},
		{"query", client.PendingTool{ToolInput: map[string]any{"query": "search term"}}, "search term"},
		{"description", client.PendingTool{ToolInput: map[string]any{"description": "a desc"}}, "a desc"},
		{"prompt", client.PendingTool{ToolInput: map[string]any{"prompt": "hello"}}, "hello"},
		{"nil input", client.PendingTool{ToolInput: nil}, ""},
		{"empty input", client.PendingTool{ToolInput: map[string]any{}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolDetail(tt.tool, 80)
			if got != tt.expect {
				t.Errorf("toolDetail = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestToolDetail_Truncation(t *testing.T) {
	tool := client.PendingTool{ToolInput: map[string]any{"command": strings.Repeat("x", 200)}}
	got := toolDetail(tool, 20)
	if len(got) > 20 {
		t.Errorf("toolDetail length = %d, want <= 20", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("truncated toolDetail should end with '...'")
	}
}

func TestToolDetail_ShortMaxLen(t *testing.T) {
	tool := client.PendingTool{ToolInput: map[string]any{"command": "long command here"}}
	// maxLen <= 5 should not truncate with "..."
	got := toolDetail(tool, 3)
	if len(got) > 17 { // original length, no truncation for small maxLen
		t.Errorf("very short maxLen: got %q", got)
	}
}

// === Iteration 10b: Safety markers ===

func TestSafetyMarker_AllTypes(t *testing.T) {
	for _, safety := range []string{"safe", "destructive", "unknown", ""} {
		got := safetyMarker(safety)
		if got == "" {
			t.Errorf("safetyMarker(%q) should not be empty", safety)
		}
	}
}

// === Iteration 10c: formatAge edge cases ===

func TestFormatAge_ExactBoundaries(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{0, "0s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{time.Hour, "1h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
		{25 * time.Hour, "1d 1h"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatAge(tt.dur); got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

// === Iteration 10d: Zoom with empty session ===

func TestRenderZoom_EmptySession(t *testing.T) {
	s := client.Session{
		SessionID: "empty",
		State:     "idle",
	}
	out := renderZoom(s, 80, 20, 0)
	if out == "" {
		t.Error("empty session should still render")
	}
}

// === Iteration 10e: Zoom with nil LastActivity ===

func TestRenderZoom_NilLastActivity(t *testing.T) {
	s := client.Session{
		SessionID:    "test",
		CWD:          "/home/test",
		State:        "running",
		PID:          123,
		LastActivity: nil,
	}
	out := renderZoom(s, 80, 20, 0)
	if out == "" {
		t.Error("nil LastActivity should still render")
	}
}

// === Iteration 10f: Zoom with all autopilot modes ===

func TestRenderZoom_AutopilotModes(t *testing.T) {
	for _, mode := range []string{"off", "on", "yolo", ""} {
		s := testSession()
		s.AutopilotMode = mode
		out := renderZoom(s, 100, 20, 0)
		if out == "" {
			t.Errorf("autopilot=%q: should produce output", mode)
		}
	}
}

// === UX polish: XML/markdown stripping in zoom ===

func TestRenderZoom_StripsXMLFromActivities(t *testing.T) {
	now := time.Now()
	s := client.Session{
		SessionID: "test-1234",
		CWD:       "/a",
		State:     "running",
		PID:       1,
		Activities: []client.Activity{
			{Timestamp: now, ActivityType: "text", Summary: "<task-notification><task-id>abc</task-id></task-notification>"},
			{Timestamp: now, ActivityType: "tool_use", Summary: "Edit: main.go"},
		},
		LastActivity: &now,
	}

	out := renderZoom(s, 100, 20, 0)
	// The CC internal message should be filtered out entirely.
	if strings.Contains(out, "<task-") {
		t.Error("zoom should filter CC internal markup from activities")
	}
	// The real activity should still be visible.
	if !strings.Contains(out, "Edit: main.go") {
		t.Error("real activities should remain visible")
	}
}

func TestRenderZoom_StripsMarkdownFromLastText(t *testing.T) {
	now := time.Now()
	s := client.Session{
		SessionID:    "test-1234",
		CWD:          "/a",
		State:        "running",
		PID:          1,
		LastText:     "**8 notes**, 3 enriched**",
		LastActivity: &now,
	}

	out := renderZoom(s, 100, 20, 0)
	if strings.Contains(out, "**") {
		t.Error("zoom should strip markdown ** from LastText")
	}
	// The cleaned text should still be present.
	if !strings.Contains(out, "8 notes") {
		t.Error("cleaned text should contain '8 notes'")
	}
}

func TestRenderZoom_StripsXMLFromLastText(t *testing.T) {
	now := time.Now()
	s := client.Session{
		SessionID:    "test-1234",
		CWD:          "/a",
		State:        "running",
		PID:          1,
		LastText:     "Result: <b>success</b>",
		LastActivity: &now,
	}

	out := renderZoom(s, 100, 20, 0)
	if strings.Contains(out, "<b>") {
		t.Error("zoom should strip XML tags from LastText")
	}
}

func TestFullView_NeverExceedsTerminalHeight(t *testing.T) {
	now := time.Now()
	sessions := []client.Session{
		{SessionID: "s1", CWD: "/a", State: "running", PID: 1, GitBranch: "main",
			LastText: strings.Repeat("long output ", 20), LastActivity: &now,
			Activities: []client.Activity{
				{Timestamp: now, ActivityType: "tool_use", Summary: "Edit: " + strings.Repeat("x", 100)},
			}},
		{SessionID: "s2", CWD: "/b", State: "idle", PID: 2},
		{SessionID: "s3", CWD: "/c", State: "waiting", PID: 3,
			PendingTools: []client.PendingTool{{ToolName: "Bash", Safety: "destructive"}}},
		{SessionID: "s4", CWD: "/d", State: "dead", PID: 4},
	}

	for _, dims := range []struct {
		w, h int
	}{
		{120, 30},
		{80, 20},
		{60, 15},
		{40, 12},
	} {
		t.Run(lipgloss.NewStyle().Render(""), func(t *testing.T) {
			m := Model{
				sessions:    sessions,
				selectedIdx: 0,
				connected:   true,
				width:       dims.w,
				height:      dims.h,
			}
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > dims.h {
				t.Errorf("View() at %dx%d produced %d lines, want <= %d",
					dims.w, dims.h, len(lines), dims.h)
			}
		})
	}
}
