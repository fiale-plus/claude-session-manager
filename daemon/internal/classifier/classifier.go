// Package classifier determines tool call safety for autopilot decisions.
package classifier

import (
	"regexp"
	"strings"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

// safeToolNames are CC tool names that are always safe (read-only or controlled-write).
var safeToolNames = map[string]bool{
	"Read":           true,
	"Glob":           true,
	"Grep":           true,
	"Edit":           true,
	"Write":          true,
	"Agent":          true,
	"TaskCreate":     true,
	"TaskUpdate":     true,
	"TaskList":       true,
	"TaskGet":        true,
	"TaskOutput":     true,
	"TaskStop":       true,
	"Skill":          true,
	"ExitPlanMode":   true,
	"EnterPlanMode":  true,
	"NotebookEdit":   true,
	"LSP":            true,
	"AskUserQuestion": true,
	"ToolSearch":     true,
	"WebFetch":       true,
	"WebSearch":      true,
	"CronCreate":     true,
	"CronDelete":     true,
	"CronList":       true,
	"EnterWorktree":  true,
	"ExitWorktree":   true,
}

// safeBashPrefixes are command prefixes considered safe.
var safeBashPrefixes = []string{
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
}

// destructivePatterns make a Bash command destructive regardless of prefix.
var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bgit\s+push\b`),
	regexp.MustCompile(`\brm\s`),
	regexp.MustCompile(`\brm$`),
	regexp.MustCompile(`\bgit\s+reset\s+--hard\b`),
	regexp.MustCompile(`\bgit\s+checkout\s+--\s`),
	regexp.MustCompile(`\bgit\s+clean\b`),
	regexp.MustCompile(`\bkill\b`),
	regexp.MustCompile(`\bDROP\b`),
	regexp.MustCompile(`\bDELETE\s+FROM\b`),
	// --force is handled specially (see isForceFlag)
	regexp.MustCompile(`--no-verify\b`),
	regexp.MustCompile(`\bnpm\s+publish\b`),
	regexp.MustCompile(`\bnpm\s+unpublish\b`),
	regexp.MustCompile(`\bnpm\s+run\s+deploy\b`),
	regexp.MustCompile(`\bcargo\s+publish\b`),
	regexp.MustCompile(`\bpip3?\s+uninstall\b`),
}

// forceRe finds --force in a command. We check manually that it's not followed by [-\w].
var forceRe = regexp.MustCompile(`--force`)

// isForceFlag returns true if command contains "--force" not followed by a hyphen or word char.
// This replaces the negative lookahead (?![-\w]) that Go's regexp doesn't support.
func isForceFlag(command string) bool {
	for _, loc := range forceRe.FindAllStringIndex(command, -1) {
		end := loc[1]
		if end >= len(command) {
			return true // --force at end of string
		}
		next := command[end]
		if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') ||
			(next >= '0' && next <= '9') || next == '_' || next == '-' {
			continue // --force-redirect, --forceful, etc.
		}
		return true
	}
	return false
}

// gitCheckoutRe matches "git checkout" at the start of a command.
var gitCheckoutRe = regexp.MustCompile(`^git\s+checkout\s+`)

// isGitCheckoutBranch returns true if command is "git checkout <branch>" (without "--").
func isGitCheckoutBranch(command string) bool {
	loc := gitCheckoutRe.FindStringIndex(command)
	if loc == nil {
		return false
	}
	rest := command[loc[1]:]
	// If the argument starts with "--", it's not a branch checkout.
	return !strings.HasPrefix(rest, "--")
}

// ClassifyTool classifies a CC tool call as safe, destructive, or unknown.
func ClassifyTool(toolName string, toolInput map[string]any) model.ToolSafety {
	if toolName == "Bash" {
		return classifyBash(toolInput)
	}
	if safeToolNames[toolName] {
		return model.SafetySafe
	}
	return model.SafetyUnknown
}

func classifyBash(toolInput map[string]any) model.ToolSafety {
	cmdRaw, ok := toolInput["command"]
	if !ok {
		return model.SafetyUnknown
	}
	command := strings.TrimSpace(cmdRaw.(string))
	if command == "" {
		return model.SafetyUnknown
	}

	// Destructive patterns override everything.
	for _, pat := range destructivePatterns {
		if pat.MatchString(command) {
			return model.SafetyDestructive
		}
	}
	// --force (not followed by hyphen/word char) is destructive.
	if isForceFlag(command) {
		return model.SafetyDestructive
	}

	// "git checkout <branch>" (no "--") is safe.
	if isGitCheckoutBranch(command) {
		return model.SafetySafe
	}

	// Check safe prefixes.
	if matchesSafePrefix(command) {
		return model.SafetySafe
	}

	return model.SafetyUnknown
}

func matchesSafePrefix(command string) bool {
	for _, prefix := range safeBashPrefixes {
		if command == prefix {
			return true
		}
		if strings.HasPrefix(command, prefix+" ") || strings.HasPrefix(command, prefix+"\t") {
			return true
		}
	}
	return false
}

// ClassifyPendingTools classifies a list of pending tool calls.
func ClassifyPendingTools(pending []model.PendingTool) []model.PendingTool {
	result := make([]model.PendingTool, len(pending))
	for i, t := range pending {
		result[i] = model.PendingTool{
			ToolName:  t.ToolName,
			ToolInput: t.ToolInput,
			Safety:    ClassifyTool(t.ToolName, t.ToolInput),
		}
	}
	return result
}
