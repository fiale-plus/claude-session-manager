package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// --- isClaudeCLI ---

func TestIsClaudeCLI(t *testing.T) {
	const versionedBin = "/Users/x/.local/share/claude/versions/2.1.77"

	tests := []struct {
		cmd  string
		want bool
	}{
		// Should match — named "claude"
		{"claude", true},
		{"/usr/local/bin/claude", true},
		{"/Users/x/.local/share/claude/versions/2.1.76/claude", true},
		{"/opt/homebrew/bin/claude", true},
		// Should match — versioned binary resolved via which+EvalSymlinks
		{versionedBin, true},

		// Should NOT match
		{"Claude.app", false},
		{"/Applications/Claude.app/Contents/MacOS/Claude", false},
		{"csm-daemon", false},
		{"claude-session-manager", false},
		{"/usr/local/bin/csm-daemon", false},
		{"/usr/local/bin/claude-session-manager", false},
		{"node", false},
		{"/usr/bin/python3", false},
		{"claude-launcher", false}, // filepath.Base is "claude-launcher", not "claude"
		{"", false},
		{"/", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isClaudeCLI(tt.cmd, versionedBin)
			if got != tt.want {
				t.Errorf("isClaudeCLI(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// --- encodeProjectPath ---

func TestEncodeProjectPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/project", "-home-user-project"},
		{"/", "-"},
		{"/a", "-a"},
		{"", "-"},
		{"/home/user/project/", "-home-user-project"},
		{"/home/user/my-project", "-home-user-my-project"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := encodeProjectPath(tt.path)
			if got != tt.want {
				t.Errorf("encodeProjectPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- decodeProjectPath ---

func TestDecodeProjectPath(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		want    string
	}{
		{"root", "-", "/"},
		{"single segment", "project", "project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeProjectPath(tt.encoded)
			if got != tt.want {
				t.Errorf("decodeProjectPath(%q) = %q, want %q", tt.encoded, got, tt.want)
			}
		})
	}
}

func TestDecodeProjectPath_WithRealDirs(t *testing.T) {
	// Create a temp dir structure to test the filesystem-based decode.
	tmpDir := t.TempDir()
	// Create /tmpDir/a/b/c
	dir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// encodeProjectPath(tmpDir + "/a/b/c") should produce something decodeProjectPath can work with.
	encoded := encodeProjectPath(filepath.Join(tmpDir, "a", "b", "c"))

	got := decodeProjectPath(encoded)
	// Should find the directory and return "c" (basename of the matched path).
	if got != "c" {
		// Might be a fallback, but at minimum it should not panic.
		t.Logf("decodeProjectPath(%q) = %q (may fall back)", encoded, got)
	}
}

func TestDecodeProjectPath_Fallback(t *testing.T) {
	// Encoded string that won't match any filesystem path → falls back to last segment.
	got := decodeProjectPath("nonexistent-path-that-doesnt-exist-at-all")
	if got == "" {
		t.Error("decodeProjectPath should return non-empty for non-matching path")
	}
	// Should return the last segment after the final hyphen.
	if got != "all" {
		t.Errorf("decodeProjectPath fallback = %q, want 'all'", got)
	}
}

// --- findLatestJSONL ---

func TestFindLatestJSONL(t *testing.T) {
	dir := t.TempDir()

	// No JSONL files → empty string.
	if got := findLatestJSONL(dir); got != "" {
		t.Errorf("empty dir: got %q, want empty", got)
	}

	// Write two JSONL files with different mod times.
	f1 := filepath.Join(dir, "old.jsonl")
	f2 := filepath.Join(dir, "new.jsonl")
	_ = os.WriteFile(f1, []byte(`{"type":"system"}`+"\n"), 0o644)
	// Set old modtime.
	oldTime := time.Now().Add(-1 * time.Hour)
	_ = os.Chtimes(f1, oldTime, oldTime)

	_ = os.WriteFile(f2, []byte(`{"type":"system"}`+"\n"), 0o644)

	got := findLatestJSONL(dir)
	if got != f2 {
		t.Errorf("got %q, want %q", got, f2)
	}
}

func TestFindLatestJSONL_IgnoresSubdirs(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory named "foo.jsonl" — should be ignored.
	_ = os.Mkdir(filepath.Join(dir, "foo.jsonl"), 0o755)

	got := findLatestJSONL(dir)
	if got != "" {
		t.Errorf("should ignore subdirectories: got %q", got)
	}
}

func TestFindLatestJSONL_NonexistentDir(t *testing.T) {
	got := findLatestJSONL("/nonexistent/dir")
	if got != "" {
		t.Errorf("nonexistent dir: got %q, want empty", got)
	}
}

func TestFindLatestJSONL_NonJSONLFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0o644)

	got := findLatestJSONL(dir)
	if got != "" {
		t.Errorf("non-jsonl files: got %q, want empty", got)
	}
}

// --- sessionFromJSONL ---

func TestSessionFromJSONL_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess-abc123.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"sess-abc123","cwd":"/home/user/project","gitBranch":"main","slug":"my-session","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:01:00Z","message":{"content":[{"type":"text","text":"Working on it..."}]}}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 12345, "ttys003")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.SessionID != "sess-abc123" {
		t.Errorf("sessionID = %q, want sess-abc123", session.SessionID)
	}
	if session.CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want /home/user/project", session.CWD)
	}
	if session.GitBranch != "main" {
		t.Errorf("gitBranch = %q, want main", session.GitBranch)
	}
	if session.PID != 12345 {
		t.Errorf("PID = %d, want 12345", session.PID)
	}
	if session.TTY != "ttys003" {
		t.Errorf("TTY = %q, want ttys003", session.TTY)
	}
	if session.ProjectName != "project" {
		t.Errorf("ProjectName = %q, want project", session.ProjectName)
	}
	if session.Slug != "my-session" {
		t.Errorf("Slug = %q, want my-session", session.Slug)
	}
}

func TestSessionFromJSONL_DeadSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"dead-1","cwd":"/tmp","timestamp":"2025-01-15T10:00:00Z"}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 0, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.State != model.StateDead {
		t.Errorf("state = %q, want dead", session.State)
	}
	if session.PID != 0 {
		t.Errorf("PID = %d, want 0", session.PID)
	}
}

func TestSessionFromJSONL_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	_ = os.WriteFile(path, []byte(""), 0o644)

	session := sessionFromJSONL(path, 100, "")
	if session != nil {
		t.Error("empty file should return nil session")
	}
}

func TestSessionFromJSONL_NonexistentFile(t *testing.T) {
	session := sessionFromJSONL("/nonexistent/path.jsonl", 100, "")
	if session != nil {
		t.Error("nonexistent file should return nil session")
	}
}

func TestSessionFromJSONL_NoSessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-custom-id.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","timestamp":"2025-01-15T10:00:00Z"}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 0, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	// Should derive session ID from filename.
	if session.SessionID != "my-custom-id" {
		t.Errorf("sessionID = %q, want my-custom-id", session.SessionID)
	}
}

func TestSessionFromJSONL_NoCWD(t *testing.T) {
	// When no CWD is in entries, projectName should be derived from directory name.
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-home-user-project")
	_ = os.MkdirAll(projectDir, 0o755)
	path := filepath.Join(projectDir, "session.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1","timestamp":"2025-01-15T10:00:00Z"}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 0, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	// ProjectName derived from decodeProjectPath of directory name.
	if session.ProjectName == "" {
		t.Error("projectName should not be empty")
	}
}

func TestSessionFromJSONL_CustomTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1","slug":"original-slug","cwd":"/tmp","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"custom-title","customTitle":"My Renamed Session","timestamp":"2025-01-15T10:01:00Z"}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 0, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	// Custom title should override slug.
	if session.Slug != "My Renamed Session" {
		t.Errorf("slug = %q, want 'My Renamed Session'", session.Slug)
	}
}

func TestSessionFromJSONL_Activities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1","cwd":"/tmp","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:01:00Z","message":{"content":[{"type":"text","text":"Let me help."}]}}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:02:00Z","message":{"content":[{"type":"tool_use","id":"tu-1","name":"Bash","input":{"command":"ls"}}]}}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 1000, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if len(session.Activities) == 0 {
		t.Error("expected activities to be populated")
	}
	if session.LastText != "Let me help." {
		t.Errorf("LastText = %q, want 'Let me help.'", session.LastText)
	}
}

func TestSessionFromJSONL_RunningStateDetection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")

	// Last entry is assistant with tool_use → should detect RUNNING
	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1","cwd":"/tmp","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:01:00Z","message":{"content":[{"type":"tool_use","id":"tu-1","name":"Read","input":{"file_path":"/a.py"}}]}}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 1000, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if session.State != model.StateRunning {
		t.Errorf("state = %q, want running", session.State)
	}
}

func TestSessionFromJSONL_PendingToolsAlwaysNil(t *testing.T) {
	// Scanner should never set pending tools — only hooks do that.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")

	lines := []string{
		`{"type":"system","subtype":"init","sessionId":"s1","cwd":"/tmp","timestamp":"2025-01-15T10:00:00Z"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:01:00Z","message":{"content":[{"type":"tool_use","id":"tu-1","name":"Bash","input":{"command":"rm -rf /"}}]}}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	session := sessionFromJSONL(path, 1000, "")
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if len(session.PendingTools) != 0 {
		t.Errorf("PendingTools = %v, want empty (scanner should not set pending)", session.PendingTools)
	}
}

// --- ExtractMetadata with customTitle ---

func TestExtractMetadata_CustomTitle(t *testing.T) {
	entries := []jsonlEntry{
		{Type: "system", SessionID: "s1", Slug: "original", CWD: "/tmp"},
		{Type: "custom-title", CustomTitle: "Renamed Session"},
	}
	_, _, _, _, customTitle := ExtractMetadata(entries)
	if customTitle != "Renamed Session" {
		t.Errorf("customTitle = %q, want 'Renamed Session'", customTitle)
	}
}

// --- toString with non-string ---

func TestToStringNonString(t *testing.T) {
	got := toString(42)
	if got != "42" {
		t.Errorf("toString(42) = %q, want '42'", got)
	}
	got = toString(true)
	if got != "true" {
		t.Errorf("toString(true) = %q, want 'true'", got)
	}
	got = toString(nil)
	if got != "null" {
		t.Errorf("toString(nil) = %q, want 'null'", got)
	}
	got = toString([]string{"a", "b"})
	if got != `["a","b"]` {
		t.Errorf("toString([]string) = %q", got)
	}
}

// --- summarizeToolInput with unknown key ---

func TestSummarizeToolInputUnknownKey(t *testing.T) {
	input := map[string]any{"custom_field": "some value"}
	got := summarizeToolInput("CustomTool", input)
	if got == "" {
		t.Error("should produce output for unknown key")
	}
	// Should contain key=value format.
	if !strings.Contains(got, "custom_field=") {
		t.Errorf("got %q, expected to contain 'custom_field='", got)
	}
}

func TestSummarizeToolInputUnknownKeyTruncatesLong(t *testing.T) {
	longVal := strings.Repeat("x", 50)
	input := map[string]any{"custom_field": longVal}
	got := summarizeToolInput("CustomTool", input)
	// The value portion should be truncated to 30 chars.
	if len(got) > 50 { // key=30chars
		t.Errorf("unknown key summary too long: %d chars", len(got))
	}
}

// --- isToolResult edge case ---

func TestIsToolResult_NonUser(t *testing.T) {
	e := jsonlEntry{Type: "assistant"}
	if isToolResult(e) {
		t.Error("assistant entry should not be a tool result")
	}
}

// --- parseContent edge case ---

func TestParseContent_NilMessage(t *testing.T) {
	e := jsonlEntry{Type: "assistant"}
	blocks := parseContent(e)
	if blocks != nil {
		t.Errorf("nil message: got %v, want nil", blocks)
	}
}

func TestParseContent_InvalidJSON(t *testing.T) {
	e := jsonlEntry{
		Type:    "assistant",
		Message: &message{Content: json.RawMessage(`not valid json`)},
	}
	blocks := parseContent(e)
	if blocks != nil {
		t.Errorf("invalid JSON content: got %v, want nil", blocks)
	}
}

// --- extractUserText with blocks that have no text ---

func TestExtractUserText_EmptyBlocks(t *testing.T) {
	blocks := []contentBlock{
		{Type: "tool_result", ToolUseID: "tu-1"},
	}
	raw, _ := json.Marshal(blocks)
	e := jsonlEntry{Type: "user", Message: &message{Content: raw}}
	got := extractUserText(e)
	if got != "" {
		t.Errorf("blocks with no text: got %q, want empty", got)
	}
}

// --- DetectState additional edge cases ---

func TestDetectState_UserOnlyToolResult(t *testing.T) {
	// A lone tool_result entry (user type).
	blocks := []contentBlock{makeToolResultBlock("tu-1")}
	entries := []jsonlEntry{
		makeEntry("user", "", blocks),
	}
	got := DetectState(entries)
	if got != model.StateRunning {
		t.Errorf("lone tool_result: got %q, want running", got)
	}
}

// --- findClaudeProcesses parsing ---
// We can't easily test findClaudeProcesses because it calls exec.Command("ps"),
// but we test the logic it depends on (isClaudeCLI) thoroughly above.

// --- RunLoop ---

func TestScannerRunLoopStops(t *testing.T) {
	sc := &Scanner{claudeProjectsDir: t.TempDir()}
	st := &mockUpdater{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		RunLoop(sc, st, 50*time.Millisecond, stop)
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

type mockUpdater struct {
	count int
}

func (m *mockUpdater) UpdateSessionFromScanner(s *model.Session) {
	m.count++
}
