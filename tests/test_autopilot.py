"""Tests for csm.autopilot — tool call classification."""

from __future__ import annotations

import pytest

from csm.autopilot import ToolSafety, classify_pending_tools, classify_tool


# ── Safe tool names (non-Bash) ──────────────────────────────────


class TestSafeToolNames:
    @pytest.mark.parametrize(
        "tool_name",
        [
            "Read",
            "Glob",
            "Grep",
            "Edit",
            "Write",
            "Agent",
            "TaskCreate",
            "TaskUpdate",
            "TaskList",
            "TaskGet",
            "TaskOutput",
            "TaskStop",
            "Skill",
            "ExitPlanMode",
            "EnterPlanMode",
            "NotebookEdit",
            "LSP",
            "AskUserQuestion",
            "ToolSearch",
            "WebFetch",
            "WebSearch",
            "CronCreate",
            "CronDelete",
            "CronList",
            "EnterWorktree",
            "ExitWorktree",
        ],
    )
    def test_known_safe_tools(self, tool_name: str):
        result = classify_tool(tool_name, {})
        assert result == ToolSafety.SAFE

    def test_unknown_tool_name_is_unknown(self):
        """Unrecognised CC tool names fall to UNKNOWN, not DESTRUCTIVE."""
        assert classify_tool("SomeNewTool", {}) == ToolSafety.UNKNOWN

    def test_empty_tool_name_is_unknown(self):
        assert classify_tool("", {}) == ToolSafety.UNKNOWN


# ── Safe Bash commands ───────────────────────────────────────────


class TestSafeBashCommands:
    @pytest.mark.parametrize(
        "command",
        [
            "ls",
            "ls -la",
            "echo hello",
            "cat foo.py",
            "head -n 10 file.txt",
            "tail -f log.txt",
            "grep -r pattern .",
            "rg pattern",
            "find . -name '*.py'",
            "python script.py",
            "python3 -m pytest",
            "pytest -x tests/",
            "npm test",
            "npx tsc --noEmit",
            "node server.js",
            "pip install -e .",
            "pip3 install requests",
            "cargo build",
            "make test",
            "go test ./...",
            "git status",
            "git diff HEAD",
            "git log --oneline -10",
            "git branch -a",
            "git show HEAD",
            "git stash",
            "git add .",
            "git commit -m 'fix bug'",
            "git fetch origin",
            "git pull",
            "git merge feature",
            "git rebase main",
            "git switch feature-branch",
            "cd /some/dir",
            "pwd",
            "which python",
            "env",
            "printenv PATH",
            "wc -l file.txt",
            "sort data.csv",
            "uniq -c sorted.txt",
            "diff a.py b.py",
            "tree src/",
            "file README.md",
            "stat foo.py",
            "du -sh .",
            "df -h",
            "uname -a",
            "date",
            "curl https://example.com",
            "wget https://example.com/file",
            "jq '.data' response.json",
            "sed 's/old/new/g' file.txt",
            "awk '{print $1}' data.txt",
            "tsc --noEmit",
            "eslint src/",
            "prettier --check src/",
            "black --check .",
            "ruff check .",
            "mypy src/",
            "flake8 src/",
            "isort --check .",
        ],
    )
    def test_safe_bash_commands(self, command: str):
        result = classify_tool("Bash", {"command": command})
        assert result == ToolSafety.SAFE, f"Expected SAFE for: {command}"

    def test_git_checkout_branch_is_safe(self):
        assert classify_tool("Bash", {"command": "git checkout main"}) == ToolSafety.SAFE
        assert classify_tool("Bash", {"command": "git checkout feature-branch"}) == ToolSafety.SAFE


# ── Destructive Bash commands ────────────────────────────────────


class TestDestructiveBashCommands:
    @pytest.mark.parametrize(
        "command",
        [
            "git push origin main",
            "git push",
            "rm file.txt",
            "rm -rf /tmp/build",
            "git reset --hard HEAD~1",
            "git checkout -- file.py",
            "git checkout -- .",
            "git clean -fd",
            "kill -9 1234",
            "echo 'DROP TABLE users'",
            "DELETE FROM users WHERE id=1",
            "git push --force origin main",
            "git commit --no-verify -m 'skip hooks'",
            # Package manager publish/deploy/uninstall
            "npm publish",
            "npm publish --access public",
            "npm unpublish my-package",
            "npm run deploy",
            "cargo publish",
            "cargo publish --dry-run",
            "pip uninstall requests",
            "pip3 uninstall -y requests",
        ],
    )
    def test_destructive_bash_commands(self, command: str):
        result = classify_tool("Bash", {"command": command})
        assert result == ToolSafety.DESTRUCTIVE, f"Expected DESTRUCTIVE for: {command}"

    def test_rm_standalone(self):
        """Bare 'rm' followed by space should be destructive."""
        assert classify_tool("Bash", {"command": "rm foo"}) == ToolSafety.DESTRUCTIVE

    def test_force_flag_anywhere(self):
        assert classify_tool("Bash", {"command": "npm install --force"}) == ToolSafety.DESTRUCTIVE

    def test_force_flag_at_end_of_command(self):
        """--force at the very end of a command (no trailing space)."""
        assert classify_tool("Bash", {"command": "git push --force"}) == ToolSafety.DESTRUCTIVE

    def test_force_hyphenated_is_not_destructive(self):
        """--force-redirect, --force-with-lease etc. should NOT match --force."""
        assert classify_tool("Bash", {"command": "curl --force-redirect http://x"}) != ToolSafety.DESTRUCTIVE
        assert classify_tool("Bash", {"command": "npm run build --force-clean"}) != ToolSafety.DESTRUCTIVE

    def test_destructive_overrides_safe_prefix(self):
        """Even if the command starts with a safe prefix, destructive patterns win."""
        # git add . && git push
        result = classify_tool("Bash", {"command": "git add . && git push origin main"})
        assert result == ToolSafety.DESTRUCTIVE

    def test_git_checkout_double_dash_is_destructive(self):
        """git checkout -- <file> discards changes, should be destructive."""
        assert classify_tool("Bash", {"command": "git checkout -- src/app.py"}) == ToolSafety.DESTRUCTIVE

    def test_npm_publish_overrides_safe_prefix(self):
        """npm publish is destructive even though npm is a safe prefix."""
        assert classify_tool("Bash", {"command": "npm publish"}) == ToolSafety.DESTRUCTIVE

    def test_pip_uninstall_overrides_safe_prefix(self):
        """pip uninstall is destructive even though pip is a safe prefix."""
        assert classify_tool("Bash", {"command": "pip uninstall flask"}) == ToolSafety.DESTRUCTIVE
        assert classify_tool("Bash", {"command": "pip3 uninstall flask"}) == ToolSafety.DESTRUCTIVE


# ── Unknown Bash commands ────────────────────────────────────────


class TestUnknownBashCommands:
    @pytest.mark.parametrize(
        "command",
        [
            "docker run -it ubuntu",
            "terraform apply",
            "ansible-playbook deploy.yml",
            "brew install something",
            "sudo apt-get update",
            "some-custom-script.sh",
        ],
    )
    def test_unknown_bash_commands(self, command: str):
        result = classify_tool("Bash", {"command": command})
        assert result == ToolSafety.UNKNOWN, f"Expected UNKNOWN for: {command}"

    def test_empty_bash_command(self):
        assert classify_tool("Bash", {"command": ""}) == ToolSafety.UNKNOWN
        assert classify_tool("Bash", {}) == ToolSafety.UNKNOWN


# ── classify_pending_tools ───────────────────────────────────────


class TestClassifyPendingTools:
    def test_classifies_list(self):
        pending = [
            ("Read", {"file_path": "/a.py"}),
            ("Bash", {"command": "git push"}),
            ("Bash", {"command": "pytest"}),
        ]
        result = classify_pending_tools(pending)
        assert len(result) == 3
        assert result[0] == ("Read", {"file_path": "/a.py"}, ToolSafety.SAFE)
        assert result[1] == ("Bash", {"command": "git push"}, ToolSafety.DESTRUCTIVE)
        assert result[2] == ("Bash", {"command": "pytest"}, ToolSafety.SAFE)

    def test_empty_list(self):
        assert classify_pending_tools([]) == []


# ── Edge cases ───────────────────────────────────────────────────


class TestEdgeCases:
    def test_command_with_leading_whitespace(self):
        assert classify_tool("Bash", {"command": "  ls -la"}) == ToolSafety.SAFE

    def test_command_with_pipe(self):
        """Safe prefix with pipe to another safe command."""
        assert classify_tool("Bash", {"command": "ls | grep foo"}) == ToolSafety.SAFE

    def test_safe_prefix_with_destructive_pipe(self):
        """If the overall command contains a destructive pattern, it's destructive."""
        assert classify_tool("Bash", {"command": "echo yes | rm -rf /"}) == ToolSafety.DESTRUCTIVE

    def test_git_push_in_middle_of_chain(self):
        """git push buried in a && chain should still be destructive."""
        result = classify_tool("Bash", {"command": "cd /tmp && git push origin main"})
        assert result == ToolSafety.DESTRUCTIVE

    def test_case_sensitivity_for_sql(self):
        """DROP and DELETE FROM are checked case-sensitively (SQL convention)."""
        # Lowercase 'drop' should NOT match (it's not the SQL keyword pattern).
        assert classify_tool("Bash", {"command": "echo drop"}) != ToolSafety.DESTRUCTIVE

    def test_no_verify_in_commit(self):
        result = classify_tool("Bash", {"command": "git commit --no-verify -m 'msg'"})
        assert result == ToolSafety.DESTRUCTIVE
