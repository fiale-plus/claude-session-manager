package classifier

import (
	"testing"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

func TestSafeToolNames(t *testing.T) {
	names := []string{
		"Read", "Glob", "Grep", "Edit", "Write", "Agent",
		"TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TaskOutput", "TaskStop",
		"Skill", "ExitPlanMode", "EnterPlanMode", "NotebookEdit", "LSP",
		"AskUserQuestion", "ToolSearch", "WebFetch", "WebSearch",
		"CronCreate", "CronDelete", "CronList", "EnterWorktree", "ExitWorktree",
	}
	for _, name := range names {
		if got := ClassifyTool(name, nil); got != model.SafetySafe {
			t.Errorf("ClassifyTool(%q) = %q, want safe", name, got)
		}
	}
}

func TestUnknownToolName(t *testing.T) {
	if got := ClassifyTool("SomeNewTool", nil); got != model.SafetyUnknown {
		t.Errorf("ClassifyTool(SomeNewTool) = %q, want unknown", got)
	}
	if got := ClassifyTool("", nil); got != model.SafetyUnknown {
		t.Errorf("ClassifyTool('') = %q, want unknown", got)
	}
}

func TestSafeBashCommands(t *testing.T) {
	commands := []string{
		"ls", "ls -la", "echo hello", "cat foo.py",
		"head -n 10 file.txt", "tail -f log.txt",
		"grep -r pattern .", "rg pattern", "find . -name '*.py'",
		"python script.py", "python3 -m pytest", "pytest -x tests/",
		"npm test", "npx tsc --noEmit", "node server.js",
		"pip install -e .", "pip3 install requests",
		"cargo build", "make test", "go test ./...",
		"git status", "git diff HEAD", "git log --oneline -10",
		"git branch -a", "git show HEAD", "git stash",
		"git add .", "git commit -m 'fix bug'",
		"git fetch origin", "git pull", "git merge feature",
		"git rebase main", "git switch feature-branch",
		"cd /some/dir", "pwd", "which python",
		"env", "printenv PATH", "wc -l file.txt",
		"sort data.csv", "uniq -c sorted.txt", "diff a.py b.py",
		"tree src/", "file README.md", "stat foo.py",
		"du -sh .", "df -h", "uname -a", "date",
		"curl https://example.com", "wget https://example.com/file",
		"jq '.data' response.json", "sed 's/old/new/g' file.txt",
		"awk '{print $1}' data.txt", "tsc --noEmit",
		"eslint src/", "prettier --check src/",
		"black --check .", "ruff check .", "mypy src/",
		"flake8 src/", "isort --check .",
	}
	for _, cmd := range commands {
		got := ClassifyTool("Bash", map[string]any{"command": cmd})
		if got != model.SafetySafe {
			t.Errorf("Bash(%q) = %q, want safe", cmd, got)
		}
	}
}

func TestGitCheckoutBranchIsSafe(t *testing.T) {
	for _, cmd := range []string{"git checkout main", "git checkout feature-branch"} {
		got := ClassifyTool("Bash", map[string]any{"command": cmd})
		if got != model.SafetySafe {
			t.Errorf("Bash(%q) = %q, want safe", cmd, got)
		}
	}
}

func TestDestructiveBashCommands(t *testing.T) {
	commands := []string{
		"git push origin main", "git push",
		"rm file.txt", "rm -rf /tmp/build",
		"git reset --hard HEAD~1",
		"git checkout -- file.py", "git checkout -- .",
		"git clean -fd", "kill -9 1234",
		"echo 'DROP TABLE users'", "DELETE FROM users WHERE id=1",
		"git push --force origin main",
		"git commit --no-verify -m 'skip hooks'",
		"npm publish", "npm publish --access public",
		"npm unpublish my-package", "npm run deploy",
		"cargo publish", "cargo publish --dry-run",
		"pip uninstall requests", "pip3 uninstall -y requests",
	}
	for _, cmd := range commands {
		got := ClassifyTool("Bash", map[string]any{"command": cmd})
		if got != model.SafetyDestructive {
			t.Errorf("Bash(%q) = %q, want destructive", cmd, got)
		}
	}
}

func TestUnknownBashCommands(t *testing.T) {
	commands := []string{
		"docker run -it ubuntu", "terraform apply",
		"ansible-playbook deploy.yml", "brew install something",
		"sudo apt-get update", "some-custom-script.sh",
	}
	for _, cmd := range commands {
		got := ClassifyTool("Bash", map[string]any{"command": cmd})
		if got != model.SafetyUnknown {
			t.Errorf("Bash(%q) = %q, want unknown", cmd, got)
		}
	}
}

func TestEmptyBashCommand(t *testing.T) {
	if got := ClassifyTool("Bash", map[string]any{"command": ""}); got != model.SafetyUnknown {
		t.Errorf("empty command = %q, want unknown", got)
	}
	if got := ClassifyTool("Bash", map[string]any{}); got != model.SafetyUnknown {
		t.Errorf("no command key = %q, want unknown", got)
	}
}

func TestDestructiveOverridesSafePrefix(t *testing.T) {
	got := ClassifyTool("Bash", map[string]any{"command": "git add . && git push origin main"})
	if got != model.SafetyDestructive {
		t.Errorf("chained push = %q, want destructive", got)
	}
}

func TestForceHyphenatedNotDestructive(t *testing.T) {
	for _, cmd := range []string{"curl --force-redirect http://x", "npm run build --force-clean"} {
		got := ClassifyTool("Bash", map[string]any{"command": cmd})
		if got == model.SafetyDestructive {
			t.Errorf("Bash(%q) = destructive, should not match --force", cmd)
		}
	}
}

func TestLeadingWhitespace(t *testing.T) {
	got := ClassifyTool("Bash", map[string]any{"command": "  ls -la"})
	if got != model.SafetySafe {
		t.Errorf("leading whitespace = %q, want safe", got)
	}
}

func TestPipeToSafeCommand(t *testing.T) {
	got := ClassifyTool("Bash", map[string]any{"command": "ls | grep foo"})
	if got != model.SafetySafe {
		t.Errorf("safe pipe = %q, want safe", got)
	}
}

func TestSafePrefixWithDestructivePipe(t *testing.T) {
	got := ClassifyTool("Bash", map[string]any{"command": "echo yes | rm -rf /"})
	if got != model.SafetyDestructive {
		t.Errorf("destructive pipe = %q, want destructive", got)
	}
}

func TestClassifyPendingTools(t *testing.T) {
	pending := []model.PendingTool{
		{ToolName: "Read", ToolInput: map[string]any{"file_path": "/a.py"}},
		{ToolName: "Bash", ToolInput: map[string]any{"command": "git push"}},
		{ToolName: "Bash", ToolInput: map[string]any{"command": "pytest"}},
	}
	result := ClassifyPendingTools(pending)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0].Safety != model.SafetySafe {
		t.Errorf("result[0] = %q, want safe", result[0].Safety)
	}
	if result[1].Safety != model.SafetyDestructive {
		t.Errorf("result[1] = %q, want destructive", result[1].Safety)
	}
	if result[2].Safety != model.SafetySafe {
		t.Errorf("result[2] = %q, want safe", result[2].Safety)
	}
}
