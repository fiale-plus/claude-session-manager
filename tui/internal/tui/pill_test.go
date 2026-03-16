package tui

import (
	"testing"

	"github.com/pchaganti/claude-session-manager/tui/internal/client"
)

// === pillName ===

func TestPillName_UserSlug(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "my-cool-setup-v2",
		ProjectName: "fallback",
		GhosttyTab:  "tab title",
	}
	if got := pillName(s); got != "my-cool-setup-v2" {
		t.Errorf("user slug: got %q, want 'my-cool-setup-v2'", got)
	}
}

func TestPillName_UserSlugFourParts(t *testing.T) {
	s := client.Session{
		SessionID: "abc12345xyz",
		Slug:      "a-b-c-d", // 4 parts = not auto slug
	}
	if got := pillName(s); got != "a-b-c-d" {
		t.Errorf("4-part slug: got %q, want 'a-b-c-d'", got)
	}
}

func TestPillName_AutoSlugFallsBehindTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "zesty-dreaming-kitten",
		GhosttyTab:  "CSM",
		ProjectName: "fallback",
	}
	if got := pillName(s); got != "CSM" {
		t.Errorf("auto slug + clean tab: got %q, want 'CSM'", got)
	}
}

func TestPillName_CommandTabFallsBackToAutoSlug(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "zesty-dreaming-kitten",
		GhosttyTab:  "cd /foo && bar",
		ProjectName: "fallback",
	}
	if got := pillName(s); got != "zesty-dreaming-kitten" {
		t.Errorf("command tab: got %q, want auto slug", got)
	}
}

func TestPillName_PipeCommandTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "active-running-test",
		GhosttyTab:  "cat file | grep pattern",
		ProjectName: "fallback",
	}
	if got := pillName(s); got != "active-running-test" {
		t.Errorf("pipe command: got %q, want auto slug", got)
	}
}

func TestPillName_SemicolonCommandTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "test-slug-name",
		GhosttyTab:  "ls; echo done",
		ProjectName: "proj",
	}
	if got := pillName(s); got != "test-slug-name" {
		t.Errorf("semicolon command: got %q, want auto slug", got)
	}
}

func TestPillName_PathTabFallsBack(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "tiny-bright-moon",
		GhosttyTab:  "/usr/local/bin/something",
		ProjectName: "fallback",
	}
	if got := pillName(s); got != "tiny-bright-moon" {
		t.Errorf("path tab: got %q, want auto slug", got)
	}
}

func TestPillName_ProjectNameFallback(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		ProjectName: "my-project",
	}
	if got := pillName(s); got != "my-project" {
		t.Errorf("no slug: got %q, want 'my-project'", got)
	}
}

func TestPillName_TruncatedSessionID(t *testing.T) {
	s := client.Session{SessionID: "abc12345xyz"}
	if got := pillName(s); got != "abc12345" {
		t.Errorf("bare session: got %q, want 'abc12345'", got)
	}
}

func TestPillName_ShortSessionID(t *testing.T) {
	s := client.Session{SessionID: "abc"}
	if got := pillName(s); got != "abc" {
		t.Errorf("short session ID: got %q, want 'abc'", got)
	}
}

func TestPillName_GhosttyTabWithIndicator(t *testing.T) {
	s := client.Session{
		SessionID:  "abc12345xyz",
		GhosttyTab: "✳ My Terminal",
	}
	if got := pillName(s); got != "My Terminal" {
		t.Errorf("indicator tab: got %q, want 'My Terminal'", got)
	}
}

func TestPillName_GhosttyTabWithDotIndicator(t *testing.T) {
	s := client.Session{
		SessionID:  "abc12345xyz",
		GhosttyTab: "● Running",
	}
	if got := pillName(s); got != "Running" {
		t.Errorf("dot indicator tab: got %q, want 'Running'", got)
	}
}

func TestPillName_EmptySlugAndEmptyTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		Slug:        "",
		GhosttyTab:  "",
		ProjectName: "",
	}
	// Falls through to truncated session ID.
	if got := pillName(s); got != "abc12345" {
		t.Errorf("all empty: got %q, want 'abc12345'", got)
	}
}

func TestPillName_CdCommandTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		GhosttyTab:  "cd /some/path",
		ProjectName: "project",
	}
	// "cd " is a command marker, so tab is skipped.
	if got := pillName(s); got != "project" {
		t.Errorf("cd command tab: got %q, want 'project'", got)
	}
}

func TestPillName_DotSlashCommandTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		GhosttyTab:  "./run.sh",
		ProjectName: "project",
	}
	if got := pillName(s); got != "project" {
		t.Errorf("dot-slash command tab: got %q, want 'project'", got)
	}
}

func TestPillName_DoubleSpaceCommandTab(t *testing.T) {
	s := client.Session{
		SessionID:   "abc12345xyz",
		GhosttyTab:  "some  command",
		ProjectName: "project",
	}
	if got := pillName(s); got != "project" {
		t.Errorf("double-space tab: got %q, want 'project'", got)
	}
}

// === isAutoSlug ===

func TestIsAutoSlug_ThreeLowercaseWords(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{"zesty-dreaming-kitten", true},
		{"hello-world-test", true},
		{"my-cool-project", true},
		{"a-b-c", true},
		{"My-Cool-Project", false},
		{"two-words", false},
		{"a-b-c-d", false},
		{"", false},
		{"hello-world-123", false},
		{"hello--world", false},
		{"-hello-world", false},
		{"hello-world-", false},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := isAutoSlug(tt.slug); got != tt.want {
				t.Errorf("isAutoSlug(%q) = %v, want %v", tt.slug, got, tt.want)
			}
		})
	}
}

// === looksLikeCommand ===

func TestLooksLikeCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"My Terminal", false},
		{"CSM", false},
		{"project-name", false},
		{"cd /foo && bar", true},
		{"cat file | grep x", true},
		{"cmd1; cmd2", true},
		{"cd /path", true},
		{"./script.sh", true},
		{"/usr/bin/thing", true},
		{"some  command", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := looksLikeCommand(tt.input); got != tt.want {
				t.Errorf("looksLikeCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// === cleanTabName ===

func TestCleanTabName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"✳ My Terminal", "My Terminal"},
		{"● Running", "Running"},
		{"○ Idle", "Idle"},
		{"◉ Active", "Active"},
		{"◎ Something", "Something"},
		{"  Spaced", "Spaced"},
		{"NormalTab", "NormalTab"},
		{"", ""},
		{"✳ ● Multiple", "Multiple"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := cleanTabName(tt.input); got != tt.want {
				t.Errorf("cleanTabName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
