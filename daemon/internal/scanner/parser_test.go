package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// --- helpers ---

// makeEntry builds a jsonlEntry with the given type, subtype, and optional message content.
func makeEntry(typ, subtype string, content any) jsonlEntry {
	e := jsonlEntry{
		Type:      typ,
		Subtype:   subtype,
		Timestamp: "2025-01-15T10:00:00Z",
	}
	if content != nil {
		raw, _ := json.Marshal(content)
		e.Message = &message{Content: raw}
	}
	return e
}

func makeToolUseBlock(id, name string, input map[string]any) contentBlock {
	return contentBlock{Type: "tool_use", ID: id, Name: name, Input: input}
}

func makeTextBlock(text string) contentBlock {
	return contentBlock{Type: "text", Text: text}
}

func makeToolResultBlock(toolUseID string) contentBlock {
	return contentBlock{Type: "tool_result", ToolUseID: toolUseID}
}

// --- DetectState tests ---

func TestDetectStateEmpty(t *testing.T) {
	if got := DetectState(nil); got != model.StateIdle {
		t.Errorf("nil entries: got %q, want idle", got)
	}
	if got := DetectState([]jsonlEntry{}); got != model.StateIdle {
		t.Errorf("empty entries: got %q, want idle", got)
	}
}

func TestDetectStateSystemEntry(t *testing.T) {
	// System entries like turn_duration → WAITING.
	entries := []jsonlEntry{
		makeEntry("system", "turn_duration", nil),
	}
	if got := DetectState(entries); got != model.StateWaiting {
		t.Errorf("system/turn_duration: got %q, want waiting", got)
	}
}

func TestDetectStateSystemSubtypes(t *testing.T) {
	subtypes := []string{"turn_duration", "stop_hook_summary", "local_command", "init"}
	for _, sub := range subtypes {
		entries := []jsonlEntry{makeEntry("system", sub, nil)}
		got := DetectState(entries)
		if got != model.StateWaiting {
			t.Errorf("system/%s: got %q, want waiting", sub, got)
		}
	}
}

func TestDetectStateAssistantWithToolUse(t *testing.T) {
	// Assistant entry with tool_use → RUNNING.
	blocks := []contentBlock{
		makeTextBlock("Let me read that file."),
		makeToolUseBlock("tu-1", "Read", map[string]any{"file_path": "/a.py"}),
	}
	entries := []jsonlEntry{
		makeEntry("assistant", "", blocks),
	}
	if got := DetectState(entries); got != model.StateRunning {
		t.Errorf("assistant+tool_use: got %q, want running", got)
	}
}

func TestDetectStateAssistantTextOnly(t *testing.T) {
	// Assistant entry with only text → WAITING.
	blocks := []contentBlock{
		makeTextBlock("All done!"),
	}
	entries := []jsonlEntry{
		makeEntry("assistant", "", blocks),
	}
	if got := DetectState(entries); got != model.StateWaiting {
		t.Errorf("assistant text only: got %q, want waiting", got)
	}
}

func TestDetectStateUserAfterSystem(t *testing.T) {
	// User message after system (new prompt) → RUNNING.
	entries := []jsonlEntry{
		makeEntry("system", "turn_duration", nil),
		makeEntry("user", "", "Fix the bug"),
	}
	if got := DetectState(entries); got != model.StateRunning {
		t.Errorf("user after system: got %q, want running", got)
	}
}

func TestDetectStateUserAfterAssistant(t *testing.T) {
	// User message after assistant (not after system) → WAITING.
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeTextBlock("Done")}),
		makeEntry("user", "", "Thanks"),
	}
	if got := DetectState(entries); got != model.StateWaiting {
		t.Errorf("user after assistant: got %q, want waiting", got)
	}
}

func TestDetectStateToolResult(t *testing.T) {
	// User entry that is a tool_result → RUNNING (CC processing result).
	blocks := []contentBlock{
		makeToolResultBlock("tu-1"),
	}
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeToolUseBlock("tu-1", "Read", nil)}),
		makeEntry("user", "", blocks),
	}
	if got := DetectState(entries); got != model.StateRunning {
		t.Errorf("tool_result: got %q, want running", got)
	}
}

func TestDetectStateFiltersMeaningless(t *testing.T) {
	// Non user/assistant/system entries should be filtered out.
	entries := []jsonlEntry{
		{Type: "metadata", Timestamp: "2025-01-15T10:00:00Z"},
		{Type: "config", Timestamp: "2025-01-15T10:00:00Z"},
	}
	if got := DetectState(entries); got != model.StateIdle {
		t.Errorf("meaningless entries: got %q, want idle", got)
	}
}

func TestDetectStateUserAlone(t *testing.T) {
	// A lone user message with no previous system → WAITING.
	entries := []jsonlEntry{
		makeEntry("user", "", "Hello"),
	}
	if got := DetectState(entries); got != model.StateWaiting {
		t.Errorf("lone user: got %q, want waiting", got)
	}
}

// --- ExtractMotivation tests ---

func TestExtractMotivationLastAssistantText(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeTextBlock("First thing")}),
		makeEntry("assistant", "", []contentBlock{makeTextBlock("Second thing")}),
	}
	got := ExtractMotivation(entries)
	if got != "Second thing" {
		t.Errorf("got %q, want %q", got, "Second thing")
	}
}

func TestExtractMotivationSkipsToolUseBlocks(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeTextBlock("I will read the file."),
			makeToolUseBlock("tu-1", "Read", nil),
		}),
	}
	got := ExtractMotivation(entries)
	if got != "I will read the file." {
		t.Errorf("got %q, want %q", got, "I will read the file.")
	}
}

func TestExtractMotivationEmpty(t *testing.T) {
	if got := ExtractMotivation(nil); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
	entries := []jsonlEntry{
		makeEntry("user", "", "Hello"),
		makeEntry("system", "turn_duration", nil),
	}
	if got := ExtractMotivation(entries); got != "" {
		t.Errorf("no assistant: got %q, want empty", got)
	}
}

func TestExtractMotivationTruncatesLongText(t *testing.T) {
	longText := strings.Repeat("x", 600)
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeTextBlock(longText)}),
	}
	got := ExtractMotivation(entries)
	if len(got) != 500 {
		t.Errorf("long text length = %d, want 500", len(got))
	}
}

// --- ExtractActivities tests ---

func TestExtractActivitiesToolUse(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeToolUseBlock("tu-1", "Read", map[string]any{"file_path": "/a.py"}),
		}),
	}
	acts := ExtractActivities(entries, 10)
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1", len(acts))
	}
	if acts[0].ActivityType != model.ActivityToolUse {
		t.Errorf("type = %q, want tool_use", acts[0].ActivityType)
	}
	if !strings.Contains(acts[0].Summary, "Read") {
		t.Errorf("summary = %q, want to contain Read", acts[0].Summary)
	}
}

func TestExtractActivitiesTextBlock(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeTextBlock("Working on it..."),
		}),
	}
	acts := ExtractActivities(entries, 10)
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1", len(acts))
	}
	if acts[0].ActivityType != model.ActivityText {
		t.Errorf("type = %q, want text", acts[0].ActivityType)
	}
}

func TestExtractActivitiesUserMessage(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("user", "", "Fix the tests"),
	}
	acts := ExtractActivities(entries, 10)
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1", len(acts))
	}
	if acts[0].ActivityType != model.ActivityUserMessage {
		t.Errorf("type = %q, want user_message", acts[0].ActivityType)
	}
}

func TestExtractActivitiesSystemEntry(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("system", "turn_duration", nil),
	}
	acts := ExtractActivities(entries, 10)
	if len(acts) != 1 {
		t.Fatalf("got %d activities, want 1", len(acts))
	}
	if acts[0].ActivityType != model.ActivitySystem {
		t.Errorf("type = %q, want system", acts[0].ActivityType)
	}
	if acts[0].Summary != "turn_duration" {
		t.Errorf("summary = %q, want turn_duration", acts[0].Summary)
	}
}

func TestExtractActivitiesSkipsToolResults(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("user", "", []contentBlock{makeToolResultBlock("tu-1")}),
	}
	acts := ExtractActivities(entries, 10)
	if len(acts) != 0 {
		t.Errorf("tool_result should be skipped, got %d activities", len(acts))
	}
}

func TestExtractActivitiesLimitN(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeTextBlock("One")}),
		makeEntry("assistant", "", []contentBlock{makeTextBlock("Two")}),
		makeEntry("assistant", "", []contentBlock{makeTextBlock("Three")}),
	}
	acts := ExtractActivities(entries, 2)
	if len(acts) != 2 {
		t.Fatalf("got %d activities, want 2", len(acts))
	}
	// Should be the last 2.
	if acts[0].Summary != "Two" {
		t.Errorf("first activity = %q, want Two", acts[0].Summary)
	}
}

func TestExtractActivitiesNoTimestamp(t *testing.T) {
	// Entries without a timestamp should be skipped.
	e := makeEntry("assistant", "", []contentBlock{makeTextBlock("No ts")})
	e.Timestamp = ""
	acts := ExtractActivities([]jsonlEntry{e}, 10)
	if len(acts) != 0 {
		t.Errorf("no-timestamp entry should be skipped, got %d", len(acts))
	}
}

func TestExtractActivitiesMixed(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("user", "", "Fix it"),
		makeEntry("assistant", "", []contentBlock{
			makeTextBlock("Let me check."),
			makeToolUseBlock("tu-1", "Bash", map[string]any{"command": "ls -la"}),
		}),
		makeEntry("user", "", []contentBlock{makeToolResultBlock("tu-1")}),
		makeEntry("system", "turn_duration", nil),
	}
	acts := ExtractActivities(entries, 20)
	// user_message + text + tool_use + system = 4 (tool_result skipped)
	if len(acts) != 4 {
		t.Fatalf("got %d activities, want 4", len(acts))
	}
	if acts[0].ActivityType != model.ActivityUserMessage {
		t.Errorf("acts[0] = %q, want user_message", acts[0].ActivityType)
	}
	if acts[1].ActivityType != model.ActivityText {
		t.Errorf("acts[1] = %q, want text", acts[1].ActivityType)
	}
	if acts[2].ActivityType != model.ActivityToolUse {
		t.Errorf("acts[2] = %q, want tool_use", acts[2].ActivityType)
	}
	if acts[3].ActivityType != model.ActivitySystem {
		t.Errorf("acts[3] = %q, want system", acts[3].ActivityType)
	}
}

// --- ExtractPendingTools tests ---

func TestExtractPendingToolsFromLastAssistant(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeToolUseBlock("tu-1", "Bash", map[string]any{"command": "ls"}),
		}),
	}
	pending := ExtractPendingTools(entries)
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1", len(pending))
	}
	if pending[0].ToolName != "Bash" {
		t.Errorf("tool name = %q, want Bash", pending[0].ToolName)
	}
}

func TestExtractPendingToolsResolvedByToolResult(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeToolUseBlock("tu-1", "Read", map[string]any{"file_path": "/a.py"}),
		}),
		makeEntry("user", "", []contentBlock{makeToolResultBlock("tu-1")}),
	}
	pending := ExtractPendingTools(entries)
	if len(pending) != 0 {
		t.Errorf("got %d pending, want 0 (should be resolved)", len(pending))
	}
}

func TestExtractPendingToolsPartiallyResolved(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeToolUseBlock("tu-1", "Read", nil),
			makeToolUseBlock("tu-2", "Bash", map[string]any{"command": "ls"}),
		}),
		makeEntry("user", "", []contentBlock{makeToolResultBlock("tu-1")}),
	}
	pending := ExtractPendingTools(entries)
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1", len(pending))
	}
	if pending[0].ToolName != "Bash" {
		t.Errorf("tool name = %q, want Bash", pending[0].ToolName)
	}
}

func TestExtractPendingToolsSystemStopsSearch(t *testing.T) {
	// If a system entry appears before finding an assistant with tool_use,
	// the turn is done — no pending tools.
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{
			makeToolUseBlock("tu-1", "Read", nil),
		}),
		makeEntry("user", "", []contentBlock{makeToolResultBlock("tu-1")}),
		makeEntry("system", "turn_duration", nil),
	}
	pending := ExtractPendingTools(entries)
	if len(pending) != 0 {
		t.Errorf("system should stop search: got %d pending, want 0", len(pending))
	}
}

func TestExtractPendingToolsNoAssistantWithToolUse(t *testing.T) {
	entries := []jsonlEntry{
		makeEntry("assistant", "", []contentBlock{makeTextBlock("All done")}),
		makeEntry("system", "turn_duration", nil),
	}
	pending := ExtractPendingTools(entries)
	if len(pending) != 0 {
		t.Errorf("no tool_use: got %d pending, want 0", len(pending))
	}
}

func TestExtractPendingToolsEmpty(t *testing.T) {
	if got := ExtractPendingTools(nil); len(got) != 0 {
		t.Errorf("nil: got %d, want 0", len(got))
	}
	if got := ExtractPendingTools([]jsonlEntry{}); len(got) != 0 {
		t.Errorf("empty: got %d, want 0", len(got))
	}
}

// --- ExtractMetadata tests ---

func TestExtractMetadataAllFields(t *testing.T) {
	entries := []jsonlEntry{
		{Type: "system", SessionID: "sess-1", Slug: "my-session", CWD: "/home/user/project", GitBranch: "main"},
	}
	sid, slug, cwd, branch := ExtractMetadata(entries)
	if sid != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", sid)
	}
	if slug != "my-session" {
		t.Errorf("slug = %q, want my-session", slug)
	}
	if cwd != "/home/user/project" {
		t.Errorf("cwd = %q, want /home/user/project", cwd)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
}

func TestExtractMetadataFromMultipleEntries(t *testing.T) {
	// Metadata is taken from last occurrence (reverse scan).
	entries := []jsonlEntry{
		{Type: "system", SessionID: "sess-1", CWD: "/old/path"},
		{Type: "assistant", SessionID: "sess-1"},
		{Type: "system", SessionID: "sess-1", CWD: "/new/path", Slug: "renamed", GitBranch: "feature"},
	}
	sid, slug, cwd, branch := ExtractMetadata(entries)
	if sid != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", sid)
	}
	if slug != "renamed" {
		t.Errorf("slug = %q, want renamed", slug)
	}
	if cwd != "/new/path" {
		t.Errorf("cwd = %q, want /new/path (last entry wins)", cwd)
	}
	if branch != "feature" {
		t.Errorf("branch = %q, want feature", branch)
	}
}

func TestExtractMetadataPartial(t *testing.T) {
	entries := []jsonlEntry{
		{Type: "system", SessionID: "sess-1"},
	}
	sid, slug, cwd, branch := ExtractMetadata(entries)
	if sid != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", sid)
	}
	if slug != "" {
		t.Errorf("slug = %q, want empty", slug)
	}
	if cwd != "" {
		t.Errorf("cwd = %q, want empty", cwd)
	}
	if branch != "" {
		t.Errorf("branch = %q, want empty", branch)
	}
}

func TestExtractMetadataEmpty(t *testing.T) {
	sid, slug, cwd, branch := ExtractMetadata(nil)
	if sid != "" || slug != "" || cwd != "" || branch != "" {
		t.Errorf("nil entries should return empty strings")
	}
}

// --- ReadTail tests ---

func TestReadTailSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1"}`,
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:01:00Z"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadTail(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Type != "system" {
		t.Errorf("entry[0].Type = %q, want system", entries[0].Type)
	}
	if entries[0].SessionID != "s1" {
		t.Errorf("entry[0].SessionID = %q, want s1", entries[0].SessionID)
	}
}

func TestReadTailLastN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, `{"type":"assistant","timestamp":"2025-01-15T10:00:00Z"}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadTail(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("got %d entries, want 5", len(entries))
	}
}

func TestReadTailLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")

	// Create a file larger than the 64KB threshold to exercise readTailSeek.
	var lines []string
	// Each line is about 80 bytes. We need >64KB = >800 lines.
	for i := 0; i < 1200; i++ {
		lines = append(lines, `{"type":"assistant","timestamp":"2025-01-15T10:00:00Z","sessionId":"large-sess"}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadTail(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("got %d entries, want 10", len(entries))
	}
	if entries[0].SessionID != "large-sess" {
		t.Errorf("sessionID = %q, want large-sess", entries[0].SessionID)
	}
}

func TestReadTailNonexistentFile(t *testing.T) {
	_, err := ReadTail("/nonexistent/path/file.jsonl", 10)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadTailInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")

	content := `not-json
{"type":"assistant","timestamp":"2025-01-15T10:00:00Z"}
also not json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ReadTail(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Only the valid JSON line should be parsed.
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Type != "assistant" {
		t.Errorf("type = %q, want assistant", entries[0].Type)
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadTail(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

// --- parseTimestamp tests ---

func TestParseTimestampFormats(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2025-01-15T10:00:00Z", true},
		{"2025-01-15T10:00:00+00:00", true},
		{"2025-01-15T10:00:00.123Z", true},
		{"2025-01-15T10:00:00.123456+00:00", true},
		{"", false},
		{"not-a-timestamp", false},
	}
	for _, tt := range tests {
		result := parseTimestamp(tt.input)
		if tt.valid && result.IsZero() {
			t.Errorf("parseTimestamp(%q) = zero, want valid time", tt.input)
		}
		if !tt.valid && !result.IsZero() {
			t.Errorf("parseTimestamp(%q) = non-zero, want zero", tt.input)
		}
	}
}

// --- splitLines tests ---

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 1},     // One empty part.
		{"a\nb", 2}, // Two parts.
		{"a\nb\n", 3}, // Three parts (trailing newline creates empty last part).
		{"a\n\nb", 3}, // Three parts (empty middle).
	}
	for _, tt := range tests {
		parts := splitLines([]byte(tt.input))
		if len(parts) != tt.want {
			t.Errorf("splitLines(%q) = %d parts, want %d", tt.input, len(parts), tt.want)
		}
	}
}

// --- helper function tests ---

func TestFilterMeaningful(t *testing.T) {
	entries := []jsonlEntry{
		{Type: "metadata"},
		{Type: "user"},
		{Type: "config"},
		{Type: "assistant"},
		{Type: "system"},
		{Type: "other"},
	}
	result := filterMeaningful(entries)
	if len(result) != 3 {
		t.Fatalf("got %d, want 3 (user, assistant, system)", len(result))
	}
	if result[0].Type != "user" || result[1].Type != "assistant" || result[2].Type != "system" {
		t.Errorf("wrong types: %v", result)
	}
}

func TestSummarizeToolInput(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"Read", map[string]any{"file_path": "/a.py"}, "/a.py"},
		{"Bash", map[string]any{"command": "ls -la"}, "ls -la"},
		{"Grep", map[string]any{"pattern": "TODO"}, "TODO"},
		{"Custom", nil, ""},
	}
	for _, tt := range tests {
		got := summarizeToolInput(tt.name, tt.input)
		if got != tt.want {
			t.Errorf("summarizeToolInput(%q, %v) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}
}

func TestSummarizeToolInputTruncatesLong(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := summarizeToolInput("Bash", map[string]any{"command": long})
	if len(got) != 60 {
		t.Errorf("length = %d, want 60", len(got))
	}
}

func TestExtractUserTextString(t *testing.T) {
	raw, _ := json.Marshal("hello world")
	e := jsonlEntry{Type: "user", Message: &message{Content: raw}}
	got := extractUserText(e)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestExtractUserTextBlocks(t *testing.T) {
	blocks := []contentBlock{
		{Type: "text", Text: "part1"},
		{Type: "text", Text: "part2"},
	}
	raw, _ := json.Marshal(blocks)
	e := jsonlEntry{Type: "user", Message: &message{Content: raw}}
	got := extractUserText(e)
	if got != "part1 part2" {
		t.Errorf("got %q, want %q", got, "part1 part2")
	}
}

func TestExtractUserTextNoMessage(t *testing.T) {
	e := jsonlEntry{Type: "user"}
	got := extractUserText(e)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
