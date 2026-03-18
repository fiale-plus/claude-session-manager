package pr

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// === claudeBin ===

func TestClaudeBin_Fallback(t *testing.T) {
	old := claudeBinFunc
	defer func() { claudeBinFunc = old }()

	claudeBinFunc = func() string { return "/test/claude" }
	if got := claudeBin(); got != "/test/claude" {
		t.Errorf("claudeBin() = %q, want /test/claude", got)
	}
}

func TestDefaultClaudeBin_LookPath(t *testing.T) {
	// Just verify it doesn't panic and returns something.
	bin := defaultClaudeBin()
	if bin == "" {
		t.Error("defaultClaudeBin() should not return empty string")
	}
}

// === buildFixCICmd ===

func TestBuildFixCICmd_Auto(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch:    "fix/thing",
		AutopilotMode: PRAuto,
		Checks: []Check{
			{Name: "ci", Conclusion: "FAILURE", Detail: "tests failed"},
			{Name: "lint", Conclusion: "SUCCESS"},
		},
	}
	cmd := buildFixCICmd(pr, "/tmp/test")

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--permission-mode acceptEdits") {
		t.Error("AUTO should use acceptEdits")
	}
	if !strings.Contains(args, "--allowedTools") {
		t.Error("AUTO should have allowedTools")
	}
	if !strings.Contains(args, "ci: tests failed") {
		t.Error("prompt should contain failing check details")
	}
	if cmd.Dir != "/tmp/test" {
		t.Errorf("Dir = %q, want /tmp/test", cmd.Dir)
	}
}

func TestBuildFixCICmd_Yolo(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch:    "fix/thing",
		AutopilotMode: PRYolo,
		Checks:        []Check{{Name: "ci", Conclusion: "FAILURE"}},
	}
	cmd := buildFixCICmd(pr, "/tmp/test")

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--permission-mode bypassPermissions") {
		t.Error("YOLO should use bypassPermissions")
	}
}

// === buildCodeReviewCmd ===

func TestBuildCodeReviewCmd(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch: "feat/new", BaseBranch: "main",
	}
	cmd := buildCodeReviewCmd(pr, "/tmp/test")

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "Read Glob Grep Bash") {
		t.Error("review should have read-only tools")
	}
	if !strings.Contains(args, "git diff main...HEAD") {
		t.Error("prompt should reference base branch diff")
	}
}

// === buildFixReviewCmd ===

func TestBuildFixReviewCmd(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch:    "fix/thing",
		AutopilotMode: PRAuto,
		ReviewFindings: []ReviewFinding{
			{Severity: SeverityCritical, File: "cmd/main.go", Line: 42, Message: "SQL injection"},
			{Severity: SeverityMinor, File: "util.go", Message: "unused var"},
		},
	}
	cmd := buildFixReviewCmd(pr, "/tmp/test")

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "SQL injection") {
		t.Error("prompt should contain critical finding")
	}
	if strings.Contains(args, "unused var") {
		t.Error("prompt should NOT contain minor finding")
	}
}

// === parseReviewOutput ===

func TestParseReviewOutput_Clean(t *testing.T) {
	findings, err := parseReviewOutput([]byte("[]"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestParseReviewOutput_WithFindings(t *testing.T) {
	input := `Here are the findings:
[
  {"severity": "critical", "file": "cmd/main.go", "line": 42, "message": "SQL injection"},
  {"severity": "important", "file": "auth.go", "message": "Missing check"},
  {"severity": "minor", "file": "util.go", "line": 10, "message": "Unused param"}
]
Done.`
	findings, err := parseReviewOutput([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("findings[0].Severity = %q, want critical", findings[0].Severity)
	}
	if findings[0].Line != 42 {
		t.Errorf("findings[0].Line = %d, want 42", findings[0].Line)
	}
}

func TestParseReviewOutput_NoJSON(t *testing.T) {
	findings, err := parseReviewOutput([]byte("The code looks great!"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("no JSON array should return empty findings, got %d", len(findings))
	}
}

func TestParseReviewOutput_InvalidJSON(t *testing.T) {
	_, err := parseReviewOutput([]byte("[{bad json}]"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseReviewOutput_Empty(t *testing.T) {
	findings, err := parseReviewOutput([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("empty output should return empty findings")
	}
}

// === stream-json flags ===

func TestBuildFixCICmd_StreamJSON(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch: "fix", AutopilotMode: PRAuto,
		Checks: []Check{{Name: "ci", Conclusion: "FAILURE"}},
	}
	args := strings.Join(buildFixCICmd(pr, "/tmp").Args, " ")
	if !strings.Contains(args, "--output-format stream-json") {
		t.Error("fix_ci should use stream-json output")
	}
	if !strings.Contains(args, "--verbose") {
		t.Error("stream-json requires --verbose")
	}
	if !strings.Contains(args, "STATUS:") {
		t.Error("prompt should contain STATUS instruction")
	}
}

func TestBuildCodeReviewCmd_StreamJSON(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch: "feat", BaseBranch: "main",
	}
	args := strings.Join(buildCodeReviewCmd(pr, "/tmp").Args, " ")
	if !strings.Contains(args, "--output-format stream-json") {
		t.Error("review should use stream-json output")
	}
	if !strings.Contains(args, "--verbose") {
		t.Error("stream-json requires --verbose")
	}
}

func TestBuildFixReviewCmd_StreamJSON(t *testing.T) {
	pr := &TrackedPR{
		Owner: "test", Repo: "repo", Number: 1,
		HeadBranch: "fix", AutopilotMode: PRAuto,
		ReviewFindings: []ReviewFinding{
			{Severity: SeverityCritical, File: "a.go", Message: "bug"},
		},
	}
	args := strings.Join(buildFixReviewCmd(pr, "/tmp").Args, " ")
	if !strings.Contains(args, "--output-format stream-json") {
		t.Error("fix_review should use stream-json output")
	}
	if !strings.Contains(args, "--verbose") {
		t.Error("stream-json requires --verbose")
	}
}

// === agentLabel ===

func TestAgentLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fix_ci", "fix-CI"},
		{"review", "review"},
		{"fix_review", "fix-review"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		if got := agentLabel(c.in); got != c.want {
			t.Errorf("agentLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// === writeAgentLog ===

func TestWriteAgentLog_CreatesFile(t *testing.T) {
	path := writeAgentLog("test/repo#1", "fix_ci", []byte("some output"), nil)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "test/repo#1") {
		t.Error("log should contain PR key")
	}
	if !strings.Contains(s, "some output") {
		t.Error("log should contain output")
	}
}

func TestWriteAgentLog_NoOutput(t *testing.T) {
	path := writeAgentLog("test/repo#2", "review", nil, fmt.Errorf("signal: killed"))
	defer os.Remove(path)

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "signal: killed") {
		t.Error("log should contain error")
	}
	if !strings.Contains(s, "(no output)") {
		t.Error("log should indicate no output")
	}
}

// === cloneForAgent (mock test) ===

func TestCloneForAgent_BadRepo(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("skipping network test in CI")
	}
	_, err := cloneForAgent("nonexistent-owner-xxx", "nonexistent-repo-xxx", "main")
	if err == nil {
		t.Error("expected error cloning nonexistent repo")
	}
}
