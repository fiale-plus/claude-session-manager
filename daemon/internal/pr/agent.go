package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// claudeBinFunc is the function used to locate the claude CLI binary.
// Tests can replace it to point at a mock script.
var claudeBinFunc = defaultClaudeBin

func claudeBin() string { return claudeBinFunc() }

func defaultClaudeBin() string {
	// Try common paths for launchd context where PATH is minimal.
	for _, p := range []string{
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		p := home + "/.local/bin/claude"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	return "claude"
}

// cloneForAgent clones the repo into a temp directory, checks out the PR
// branch, and returns the work directory. Caller must clean up via os.RemoveAll.
func cloneForAgent(owner, repo, branch string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "ccc-agent-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	repoSlug := fmt.Sprintf("%s/%s", owner, repo)
	cmd := exec.Command(ghBin(), "repo", "clone", repoSlug, tmpDir,
		"--", "--branch", branch, "--single-branch", "--depth=50")
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("clone %s branch %s: %v: %s", repoSlug, branch, err, output)
	}
	return tmpDir, nil
}

// --- command builders ---

func buildFixCICmd(pr *TrackedPR, workDir string) *exec.Cmd {
	var failing []string
	for _, c := range pr.Checks {
		if c.Conclusion == "FAILURE" {
			s := c.Name
			if c.Detail != "" {
				s += ": " + c.Detail
			}
			failing = append(failing, "- "+s)
		}
	}

	prompt := fmt.Sprintf(
		"CI checks are failing on PR #%d in %s/%s (branch %s).\n\n"+
			"Failing checks:\n%s\n\n"+
			"Steps:\n"+
			"1. Read the failing check logs if available\n"+
			"2. Identify and fix the root cause\n"+
			"3. Run tests locally to verify the fix\n"+
			"4. Commit and push to the current branch\n\n"+
			"Do not change test expectations unless the test itself is wrong.",
		pr.Number, pr.Owner, pr.Repo, pr.HeadBranch,
		strings.Join(failing, "\n"),
	)

	args := []string{
		"-p", prompt,
		"--no-session-persistence",
		"--max-budget-usd", "5",
		"--model", "sonnet",
	}

	switch pr.AutopilotMode {
	case PRYolo:
		args = append(args, "--permission-mode", "bypassPermissions")
	default:
		args = append(args,
			"--permission-mode", "acceptEdits",
			"--allowedTools", "Bash Edit Write Read Glob Grep",
		)
	}

	cmd := exec.Command(claudeBin(), args...)
	cmd.Dir = workDir
	return cmd
}

func buildCodeReviewCmd(pr *TrackedPR, workDir string) *exec.Cmd {
	prompt := fmt.Sprintf(
		"Review the changes on this branch (%s → %s) for code quality.\n\n"+
			"Use `git diff %s...HEAD` to see the changes.\n\n"+
			"Output ONLY a JSON array of findings. Each finding:\n"+
			`{"severity": "critical|important|minor", "file": "path", "line": 42, "message": "description"}`+"\n\n"+
			"Focus on: bugs, security issues, correctness, missing error handling, logic errors.\n"+
			"Do NOT flag style, formatting, or documentation issues.\n"+
			"If the code is clean, output: []\n"+
			"Output the JSON array and nothing else.",
		pr.HeadBranch, pr.BaseBranch, pr.BaseBranch,
	)

	args := []string{
		"-p", prompt,
		"--no-session-persistence",
		"--max-budget-usd", "3",
		"--model", "sonnet",
		"--allowedTools", "Read Glob Grep Bash",
	}

	cmd := exec.Command(claudeBin(), args...)
	cmd.Dir = workDir
	return cmd
}

func buildFixReviewCmd(pr *TrackedPR, workDir string) *exec.Cmd {
	var issues []string
	for _, f := range pr.ReviewFindings {
		if f.Severity != SeverityCritical && f.Severity != SeverityImportant {
			continue
		}
		loc := f.File
		if f.Line > 0 {
			loc += fmt.Sprintf(":%d", f.Line)
		}
		issues = append(issues, fmt.Sprintf("- [%s] %s — %s", f.Severity, loc, f.Message))
	}

	prompt := fmt.Sprintf(
		"Code review found the following issues on this PR. Fix them:\n\n%s\n\n"+
			"After fixing:\n"+
			"1. Run tests to verify nothing is broken\n"+
			"2. Commit and push to the current branch",
		strings.Join(issues, "\n"),
	)

	args := []string{
		"-p", prompt,
		"--no-session-persistence",
		"--max-budget-usd", "5",
		"--model", "sonnet",
	}

	switch pr.AutopilotMode {
	case PRYolo:
		args = append(args, "--permission-mode", "bypassPermissions")
	default:
		args = append(args,
			"--permission-mode", "acceptEdits",
			"--allowedTools", "Bash Edit Write Read Glob Grep",
		)
	}

	cmd := exec.Command(claudeBin(), args...)
	cmd.Dir = workDir
	return cmd
}

// --- spawn functions ---

const agentTimeout = 15 * time.Minute

func (p *Poller) spawnFixCI(pr *TrackedPR) {
	key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
	owner, repo, branch := pr.Owner, pr.Repo, pr.HeadBranch

	go func() {
		workDir, err := cloneForAgent(owner, repo, branch)
		if err != nil {
			p.agentComplete(key, "fix_ci", err, nil)
			return
		}
		defer os.RemoveAll(workDir)

		ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
		defer cancel()

		cmd := buildFixCICmd(pr, workDir)
		cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
		cmd.Dir = workDir

		output, err := cmd.CombinedOutput()
		p.agentComplete(key, "fix_ci", err, output)
	}()
}

func (p *Poller) spawnCodeReview(pr *TrackedPR) {
	key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
	owner, repo, branch := pr.Owner, pr.Repo, pr.HeadBranch

	go func() {
		workDir, err := cloneForAgent(owner, repo, branch)
		if err != nil {
			p.agentComplete(key, "review", err, nil)
			return
		}
		defer os.RemoveAll(workDir)

		ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
		defer cancel()

		cmd := buildCodeReviewCmd(pr, workDir)
		cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
		cmd.Dir = workDir

		output, err := cmd.CombinedOutput()
		p.agentComplete(key, "review", err, output)
	}()
}

func (p *Poller) spawnFixReview(pr *TrackedPR) {
	key := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
	owner, repo, branch := pr.Owner, pr.Repo, pr.HeadBranch

	go func() {
		workDir, err := cloneForAgent(owner, repo, branch)
		if err != nil {
			p.agentComplete(key, "fix_review", err, nil)
			return
		}
		defer os.RemoveAll(workDir)

		ctx, cancel := context.WithTimeout(context.Background(), agentTimeout)
		defer cancel()

		cmd := buildFixReviewCmd(pr, workDir)
		cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
		cmd.Dir = workDir

		output, err := cmd.CombinedOutput()
		p.agentComplete(key, "fix_review", err, output)
	}()
}

// --- completion callback ---

func (p *Poller) agentComplete(key, agentType string, err error, output []byte) {
	p.mu.Lock()
	pr, ok := p.tracked[key]
	if !ok {
		p.mu.Unlock()
		return
	}

	pr.AgentRunning = ""

	if err != nil {
		pr.Timeline = append(pr.Timeline, PREvent{
			Time: time.Now(), Icon: "✗",
			Message: fmt.Sprintf("Agent %s failed: %v", agentType, err),
		})
		log.Printf("pr: agent %s for %s failed: %v", agentType, key, err)
	} else {
		msg := fmt.Sprintf("Agent %s completed", agentType)

		if agentType == "review" && output != nil {
			findings, parseErr := parseReviewOutput(output)
			if parseErr != nil {
				log.Printf("pr: review parse failed for %s: %v", key, parseErr)
				pr.ReviewState = "clean"
			} else {
				pr.ReviewFindings = findings
				if pr.HasActionableFindings() {
					pr.ReviewState = "has_issues"
					actionable := 0
					for _, f := range findings {
						if f.Severity == SeverityCritical || f.Severity == SeverityImportant {
							actionable++
						}
					}
					msg += fmt.Sprintf(" — %d actionable issues", actionable)
				} else {
					pr.ReviewState = "clean"
					msg += " — clean"
				}
			}
		}

		if agentType == "fix_review" {
			// After fixing review issues, reset for re-review.
			pr.ReviewState = ""
			pr.ReviewFindings = nil
		}

		pr.Timeline = append(pr.Timeline, PREvent{
			Time: time.Now(), Icon: "🤖", Message: msg,
		})
		log.Printf("pr: %s for %s", msg, key)
	}

	p.save()
	p.mu.Unlock()

	if p.onChange != nil {
		p.onChange()
	}
}

// --- output parsing ---

// parseReviewOutput extracts ReviewFindings from claude -p output.
// The model is instructed to output a raw JSON array of findings.
func parseReviewOutput(output []byte) ([]ReviewFinding, error) {
	text := strings.TrimSpace(string(output))

	// The output may have surrounding text; extract the JSON array.
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end <= start {
		// No JSON array found — treat as clean.
		return nil, nil
	}
	jsonStr := text[start : end+1]

	var findings []ReviewFinding
	if err := json.Unmarshal([]byte(jsonStr), &findings); err != nil {
		return nil, fmt.Errorf("parse findings JSON: %w", err)
	}
	return findings, nil
}
