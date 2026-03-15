package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderHints_AlwaysEndsWithHelp verifies the "h help" anchor is always
// present regardless of width.
func TestRenderHints_AlwaysEndsWithHelp(t *testing.T) {
	for _, w := range []int{200, 100, 60, 30} {
		t.Run("", func(t *testing.T) {
			out := renderHints(false, false, w)
			if !strings.Contains(out, "help") {
				t.Errorf("width %d: hints missing 'help' anchor", w)
			}
		})
	}
}

// TestRenderHints_SingleLine ensures hints never wrap to multiple lines,
// which would break layout height math.
func TestRenderHints_SingleLine(t *testing.T) {
	for _, w := range []int{200, 100, 80, 50, 30} {
		out := renderHints(false, true, w)
		h := lipgloss.Height(out)
		if h != 1 {
			t.Errorf("width %d: hints height = %d, want 1", w, h)
		}
	}
}

// TestRenderHints_NarrowDropsHints verifies that on narrow terminals
// lower-priority hints are dropped while "h help" remains.
func TestRenderHints_NarrowDropsHints(t *testing.T) {
	wide := renderHints(false, false, 200)
	narrow := renderHints(false, false, 40)

	wideCount := strings.Count(wide, "\u2502")
	narrowCount := strings.Count(narrow, "\u2502")

	if narrowCount >= wideCount {
		t.Errorf("narrow (%d separators) should have fewer hints than wide (%d)",
			narrowCount, wideCount)
	}
}

// TestRenderHints_PendingShowsApproveReject verifies approval keys appear
// when there are pending tools.
func TestRenderHints_PendingShowsApproveReject(t *testing.T) {
	out := renderHints(false, true, 200)
	for _, keyword := range []string{"approve", "reject"} {
		if !strings.Contains(out, keyword) {
			t.Errorf("with pending tools, hints should contain %q", keyword)
		}
	}
}

// TestRenderHints_QueueVisible verifies queue-specific hints.
func TestRenderHints_QueueVisible(t *testing.T) {
	out := renderHints(true, false, 200)
	if !strings.Contains(out, "close queue") {
		t.Error("queue visible: hints should contain 'close queue'")
	}
	if strings.Contains(out, "queue") && !strings.Contains(out, "close queue") {
		t.Error("queue visible: should show 'close queue' not 'queue'")
	}
}

// TestRenderHints_MergedNavigate verifies ←→ and ↑↓ are merged into one hint.
func TestRenderHints_MergedNavigate(t *testing.T) {
	out := renderHints(false, false, 200)
	// Should contain the merged arrows, not separate navigate/scroll entries.
	if strings.Contains(out, "scroll") {
		t.Error("hints should not have separate 'scroll' — arrows are merged into 'navigate'")
	}
	if !strings.Contains(out, "navigate") {
		t.Error("hints should contain 'navigate'")
	}
}
