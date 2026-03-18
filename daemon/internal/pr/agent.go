package pr

import (
	"bufio"
	"bytes"
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

// statusInstruction is appended to all agent prompts to get live status updates.
const statusInstruction = "\n\nIMPORTANT: At each major step, print a short status line starting with " +
	"\"STATUS: \" (e.g. \"STATUS: reading CI logs\", \"STATUS: found root cause in foo.go\", " +
	"\"STATUS: running tests\", \"STATUS: pushing fix\"). These are shown in a live dashboard."

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
	) + statusInstruction

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
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
	) + statusInstruction

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
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
	) + statusInstruction

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json", "--verbose",
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

// --- streaming agent runner ---

// streamEvent is the minimal structure for parsing claude stream-json events.
type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Result   string  `json:"result"`
	CostUSD  float64 `json:"total_cost_usd"`
	Duration float64 `json:"duration_ms"`
}

// runStreamingAgent runs a claude -p command with stream-json output,
// parsing STATUS: lines and forwarding them to the PR timeline in real-time.
// Returns the final result text and accumulated full output for logging.
func (p *Poller) runStreamingAgent(ctx context.Context, cmd *exec.Cmd, key, agentType string) (result string, allOutput []byte, err error) {
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return "", nil, fmt.Errorf("stdout pipe: %w", pipeErr)
	}
	// Capture stderr separately for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if startErr := cmd.Start(); startErr != nil {
		return "", nil, fmt.Errorf("start: %w", startErr)
	}

	var fullOutput bytes.Buffer
	scanner := bufio.NewScanner(stdout)
	// stream-json can have long lines (tool results with file contents).
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	// Live stream log — write every event to a file for debugging.
	safe := strings.NewReplacer("/", "-", "#", "-").Replace(key)
	streamLogPath := fmt.Sprintf("/tmp/csm-agent-%s-%s-stream.log", safe, agentType)
	streamLog, _ := os.Create(streamLogPath)
	defer func() {
		if streamLog != nil {
			streamLog.Close()
		}
	}()
	log.Printf("pr: agent %s stream log: %s", agentType, streamLogPath)

	for scanner.Scan() {
		line := scanner.Bytes()
		fullOutput.Write(line)
		fullOutput.WriteByte('\n')
		if streamLog != nil {
			streamLog.Write(line)
			streamLog.WriteString("\n")
			streamLog.Sync()
		}

		var ev streamEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}

		switch ev.Type {
		case "assistant":
			// Look for STATUS: lines in assistant text content.
			for _, block := range ev.Message.Content {
				if block.Type != "text" {
					continue
				}
				for _, textLine := range strings.Split(block.Text, "\n") {
					trimmed := strings.TrimSpace(textLine)
					if strings.HasPrefix(trimmed, "STATUS:") {
						status := strings.TrimSpace(strings.TrimPrefix(trimmed, "STATUS:"))
						if status != "" {
							p.agentProgress(key, agentType, status)
						}
					}
				}
			}
		case "result":
			result = ev.Result
			if ev.CostUSD > 0 {
				p.agentCostUpdate(key, ev.CostUSD)
			}
		}
	}

	waitErr := cmd.Wait()

	// Append stderr to output for logging.
	if stderrBuf.Len() > 0 {
		fullOutput.WriteString("\n--- stderr ---\n")
		fullOutput.Write(stderrBuf.Bytes())
	}

	return result, fullOutput.Bytes(), waitErr
}

// agentLabel returns a human-friendly label for a timeline prefix.
func agentLabel(agentType string) string {
	switch agentType {
	case "fix_ci":
		return "fix-CI"
	case "review":
		return "review"
	case "fix_review":
		return "fix-review"
	default:
		return agentType
	}
}

// agentProgress adds a status update to the PR timeline from a running agent.
func (p *Poller) agentProgress(key, agentType, status string) {
	p.mu.Lock()
	pr, ok := p.tracked[key]
	if ok {
		pr.Timeline = append(pr.Timeline, PREvent{
			Time: time.Now(), Icon: "⚙",
			Message: agentLabel(agentType) + ": " + status,
		})
		p.save()
	}
	p.mu.Unlock()

	if ok && p.onChange != nil {
		p.onChange()
	}
}

// agentCostUpdate records the agent cost on the PR.
func (p *Poller) agentCostUpdate(key string, costUSD float64) {
	p.mu.Lock()
	pr, ok := p.tracked[key]
	if ok {
		pr.AgentCostUSD += costUSD
	}
	p.mu.Unlock()
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

		_, output, err := p.runStreamingAgent(ctx, cmd, key, "fix_ci")
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

		result, output, err := p.runStreamingAgent(ctx, cmd, key, "review")
		// For review, the result field contains the final text output.
		// Pass it as output for parseReviewOutput.
		if err == nil && result != "" {
			output = []byte(result)
		}
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

		_, output, err := p.runStreamingAgent(ctx, cmd, key, "fix_review")
		p.agentComplete(key, "fix_review", err, output)
	}()
}

// writeAgentLog writes agent output + error to /tmp/csm-agent-<key>-<type>.log.
// Returns the log path for use in the daemon log line.
func writeAgentLog(key, agentType string, output []byte, runErr error) string {
	// Sanitize key for use in filename (replace / and # with -).
	safe := strings.NewReplacer("/", "-", "#", "-").Replace(key)
	path := fmt.Sprintf("/tmp/csm-agent-%s-%s.log", safe, agentType)
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("=== CSM agent log: %s %s ===\n", key, agentType))
	buf.WriteString(fmt.Sprintf("error: %v\n", runErr))
	buf.WriteString("--- output ---\n")
	if len(output) > 0 {
		buf.Write(output)
	} else {
		buf.WriteString("(no output)\n")
	}
	_ = os.WriteFile(path, []byte(buf.String()), 0o644)
	return path
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
		logFile := writeAgentLog(key, agentType, output, err)
		log.Printf("pr: agent %s for %s failed: %v (log: %s)", agentType, key, err, logFile)
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
