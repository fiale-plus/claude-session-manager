"""Tool call classification and auto-approve logic for autopilot mode."""

from __future__ import annotations

import re
from enum import Enum


class ToolSafety(Enum):
    SAFE = "safe"
    DESTRUCTIVE = "destructive"
    UNKNOWN = "unknown"


# Tool names that are always safe (read-only or controlled-write operations).
_SAFE_TOOL_NAMES: frozenset[str] = frozenset({
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
    "Skill",
    "ExitPlanMode",
    "EnterPlanMode",
    "NotebookEdit",
    "LSP",
    "AskUserQuestion",
})

# Bash command prefixes considered safe (read-only, dev-tooling, or
# version-control operations that don't push/destroy).
_SAFE_BASH_PREFIXES: tuple[str, ...] = (
    "ls",
    "echo",
    "cat",
    "head",
    "tail",
    "grep",
    "rg",
    "find",
    "python",
    "python3",
    "pytest",
    "npm",
    "npx",
    "node",
    "pip",
    "pip3",
    "cargo",
    "make",
    "go",
    "git status",
    "git diff",
    "git log",
    "git branch",
    "git show",
    "git stash",
    "git add",
    "git commit",
    "git fetch",
    "git pull",
    "git merge",
    "git rebase",
    "git switch",
    "cd",
    "pwd",
    "which",
    "env",
    "printenv",
    "wc",
    "sort",
    "uniq",
    "diff",
    "tree",
    "file",
    "stat",
    "du",
    "df",
    "uname",
    "date",
    "curl",
    "wget",
    "jq",
    "sed",
    "awk",
    "tsc",
    "eslint",
    "prettier",
    "black",
    "ruff",
    "mypy",
    "flake8",
    "isort",
)

# Patterns that make a Bash command destructive regardless of prefix.
_DESTRUCTIVE_PATTERNS: tuple[re.Pattern[str], ...] = (
    re.compile(r"\bgit\s+push\b"),
    re.compile(r"\brm\s"),
    re.compile(r"\brm\b$"),
    re.compile(r"\bgit\s+reset\s+--hard\b"),
    re.compile(r"\bgit\s+checkout\s+--\s"),
    re.compile(r"\bgit\s+clean\b"),
    re.compile(r"\bkill\b"),
    re.compile(r"\bDROP\b"),
    re.compile(r"\bDELETE\s+FROM\b"),
    re.compile(r"--force\b"),
    re.compile(r"--no-verify\b"),
)

# "git checkout <branch>" (without "--") is safe; handled by checking
# that `git checkout` is NOT followed by ` -- `.
_SAFE_GIT_CHECKOUT_RE = re.compile(r"^git\s+checkout\s+(?!--)(\S+)")


def classify_tool(tool_name: str, tool_input: dict) -> ToolSafety:
    """Classify a CC tool call as safe, destructive, or unknown.

    - DESTRUCTIVE: Bash commands matching the blocklist, or unrecognised tool names.
    - SAFE: Known tool names or Bash commands matching the safe-prefix list.
    - UNKNOWN: Bash commands not matching either list.
    """
    if tool_name == "Bash":
        return _classify_bash(tool_input)

    if tool_name in _SAFE_TOOL_NAMES:
        return ToolSafety.SAFE

    # Unrecognised tool name — treat as destructive.
    return ToolSafety.DESTRUCTIVE


def _classify_bash(tool_input: dict) -> ToolSafety:
    """Classify a Bash tool call by inspecting its command string."""
    command = str(tool_input.get("command", "")).strip()
    if not command:
        return ToolSafety.UNKNOWN

    # Check destructive patterns first — they override everything.
    for pattern in _DESTRUCTIVE_PATTERNS:
        if pattern.search(command):
            return ToolSafety.DESTRUCTIVE

    # Special handling: `git checkout <branch>` (no `--`) is safe.
    if _SAFE_GIT_CHECKOUT_RE.match(command):
        return ToolSafety.SAFE

    # Check safe prefixes.  We match the first token(s) of the command
    # against each prefix.  Prefixes can be multi-word (e.g. "git status").
    if _matches_safe_prefix(command):
        return ToolSafety.SAFE

    return ToolSafety.UNKNOWN


def _matches_safe_prefix(command: str) -> bool:
    """Return True if *command* starts with one of the safe prefixes."""
    for prefix in _SAFE_BASH_PREFIXES:
        if command == prefix:
            return True
        # Prefix followed by a space or common shell operators.
        if command.startswith(prefix + " ") or command.startswith(prefix + "\t"):
            return True
        # Handle commands that start with env vars or `cd &&` chains:
        # we only match at the very start.
        if " " not in prefix:
            # Single-word prefix: also match after path qualification.
            # e.g. "/usr/bin/python3 foo.py" should not match, but
            # "python3 foo.py" should.  We already handle that above.
            pass
    return False


def classify_pending_tools(
    pending: list[tuple[str, dict]],
) -> list[tuple[str, dict, ToolSafety]]:
    """Classify a list of pending tool calls."""
    return [
        (name, inp, classify_tool(name, inp))
        for name, inp in pending
    ]
