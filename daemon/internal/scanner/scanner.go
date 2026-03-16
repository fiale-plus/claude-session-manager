package scanner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/model"
)

const recentThresholdHours = 24

// Scanner discovers Claude Code sessions via process table and JSONL files.
type Scanner struct {
	claudeProjectsDir string
}

// New creates a scanner.
func New() *Scanner {
	home, _ := os.UserHomeDir()
	return &Scanner{
		claudeProjectsDir: filepath.Join(home, ".claude", "projects"),
	}
}

// Discover finds all active and recently-dead CC sessions.
func (s *Scanner) Discover() []*model.Session {
	var sessions []*model.Session
	seenJSONL := make(map[string]bool)

	// Phase 1: Running processes.
	procs := findClaudeProcesses()
	cwdToPID := make(map[string]procInfo)
	for _, p := range procs {
		if existing, ok := cwdToPID[p.cwd]; !ok || p.pid > existing.pid {
			cwdToPID[p.cwd] = p
		}
	}

	for cwd, proc := range cwdToPID {
		var jsonlPath string
		// If we have a session ID from --resume, try to match to a specific JSONL file.
		if proc.sessionID != "" {
			encoded := encodeProjectPath(cwd)
			projectDir := filepath.Join(s.claudeProjectsDir, encoded)
			candidate := filepath.Join(projectDir, proc.sessionID+".jsonl")
			if _, err := os.Stat(candidate); err == nil {
				jsonlPath = candidate
			}
		}
		if jsonlPath == "" {
			encoded := encodeProjectPath(cwd)
			projectDir := filepath.Join(s.claudeProjectsDir, encoded)
			jsonlPath = findLatestJSONL(projectDir)
		}
		if jsonlPath == "" {
			continue
		}
		if seenJSONL[jsonlPath] {
			continue
		}

		session := sessionFromJSONL(jsonlPath, proc.pid, proc.tty)
		if session == nil {
			continue
		}
		sessions = append(sessions, session)
		seenJSONL[jsonlPath] = true
	}

	// Phase 2: Dead/historical sessions.
	entries, err := os.ReadDir(s.claudeProjectsDir)
	if err == nil {
		cutoff := time.Now().Add(-recentThresholdHours * time.Hour)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			projectDir := filepath.Join(s.claudeProjectsDir, entry.Name())
			jsonlPath := findLatestJSONL(projectDir)
			if jsonlPath == "" || seenJSONL[jsonlPath] {
				continue
			}
			info, err := os.Stat(jsonlPath)
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			session := sessionFromJSONL(jsonlPath, 0, "")
			if session == nil {
				continue
			}
			sessions = append(sessions, session)
		}
	}

	return sessions
}

type procInfo struct {
	pid       int
	cwd       string
	sessionID string
	tty       string
}

// isClaudeCLI returns true if cmd looks like the actual `claude` CLI binary,
// excluding Claude.app, csm-daemon, and claude-session-manager.
func isClaudeCLI(cmd string) bool {
	// Exclude known non-CLI binaries first.
	if strings.Contains(cmd, "Claude.app") {
		return false
	}
	if strings.Contains(cmd, "csm-daemon") || strings.Contains(cmd, "claude-session-manager") {
		return false
	}
	// Match any path ending in "claude" as the binary name.
	// filepath.Base handles all path formats:
	//   "claude" → "claude"
	//   "/usr/local/bin/claude" → "claude"
	//   "/Users/x/.local/share/claude/versions/2.1.76/claude" → "claude"
	return filepath.Base(cmd) == "claude"
}

func findClaudeProcesses() []procInfo {
	// Use ps to find Claude CLI processes with TTY info.
	out, err := exec.Command("ps", "-eo", "pid,tty,args").Output()
	if err != nil {
		return nil
	}

	type candidate struct {
		pid       int
		tty       string
		sessionID string
	}
	var candidates []candidate

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "  PID TTY      COMMAND..."
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(fields[0], "%d", &pid); err != nil {
			continue
		}
		tty := fields[1] // e.g. "ttys003" or "??"
		if tty == "??" {
			tty = ""
		}
		// The command starts at fields[2].
		if !isClaudeCLI(fields[2]) {
			continue
		}
		// Extract session ID from --resume flag.
		var sid string
		for i, arg := range fields[2:] {
			if arg == "--resume" && i+1 < len(fields[2:]) {
				sid = fields[2:][i+1]
				break
			}
			if strings.HasPrefix(arg, "--resume=") {
				sid = strings.TrimPrefix(arg, "--resume=")
				break
			}
		}
		candidates = append(candidates, candidate{pid: pid, tty: tty, sessionID: sid})
	}

	var result []procInfo
	for _, c := range candidates {
		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", c.pid))
		if err != nil {
			// macOS: use lsof with -a flag to AND filters.
			cwd = getCWDMacOS(c.pid)
		}
		if cwd == "" {
			continue
		}
		result = append(result, procInfo{pid: c.pid, cwd: cwd, sessionID: c.sessionID, tty: c.tty})
	}
	return result
}

func getCWDMacOS(pid int) string {
	out, err := exec.Command("lsof", "-a", "-d", "cwd", "-p", fmt.Sprintf("%d", pid), "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") && strings.HasPrefix(line[1:], "/") {
			return line[1:]
		}
	}
	return ""
}

func encodeProjectPath(path string) string {
	clean := strings.TrimRight(path, "/")
	if clean == "" {
		return "-"
	}
	return strings.ReplaceAll(clean, "/", "-")
}

// decodeProjectPath attempts to reconstruct a filesystem path from a Claude
// projects directory name (where / is encoded as -). Each - could be either
// a path separator or a literal hyphen. We try all possibilities and verify
// against the filesystem.
func decodeProjectPath(encoded string) string {
	if encoded == "-" {
		return "/"
	}
	// Fast path: simple replacement, check if it exists.
	simple := "/" + strings.ReplaceAll(encoded, "-", "/")
	if info, err := os.Stat(simple); err == nil && info.IsDir() {
		return filepath.Base(simple)
	}

	// Recursive decode: try each - as either / or literal -.
	parts := strings.Split(encoded, "-")
	if len(parts) <= 1 {
		return encoded
	}

	var best string
	var tryDecode func(idx int, current string)
	tryDecode = func(idx int, current string) {
		if idx >= len(parts) {
			path := "/" + current
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				if best == "" || len(path) > len(best) {
					best = path
				}
			}
			return
		}
		if idx == 0 {
			tryDecode(idx+1, parts[0])
			return
		}
		// Try as path separator.
		tryDecode(idx+1, current+"/"+parts[idx])
		// Try as literal hyphen.
		tryDecode(idx+1, current+"-"+parts[idx])
	}
	tryDecode(0, "")

	if best != "" {
		return filepath.Base(best)
	}
	// Fallback: last segment after the final -.
	return parts[len(parts)-1]
}

func findLatestJSONL(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var latest string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = filepath.Join(dir, entry.Name())
			latestTime = info.ModTime()
		}
	}
	return latest
}

func sessionFromJSONL(jsonlPath string, pid int, tty string) *model.Session {
	entries, err := ReadTail(jsonlPath, 50)
	if err != nil || len(entries) == 0 {
		return nil
	}

	sessionID, slug, cwd, gitBranch, customTitle := ExtractMetadata(entries)
	if sessionID == "" {
		sessionID = filepath.Base(strings.TrimSuffix(jsonlPath, ".jsonl"))
	}

	// Use customTitle from /rename as the slug if available.
	// CC writes custom-title entries but never updates the slug field.
	if customTitle != "" {
		slug = customTitle
	}

	projectName := ""
	if cwd != "" {
		projectName = filepath.Base(cwd)
	} else {
		dirName := filepath.Base(filepath.Dir(jsonlPath))
		projectName = decodeProjectPath(dirName)
	}

	var st model.SessionState
	if pid > 0 {
		st = DetectState(entries)
	} else {
		st = model.StateDead
	}

	activities := ExtractActivities(entries, 8)
	motivation := ExtractMotivation(entries)

	// Scanner cannot reliably detect pending tools from JSONL —
	// an unmatched tool_use could be executing (not blocked on permission).
	// Only live hooks (PreToolUse) can detect real permission prompts.
	var pendingTools []model.PendingTool

	var lastActivity *time.Time
	if len(activities) > 0 {
		t := activities[len(activities)-1].Timestamp
		lastActivity = &t
	}

	return &model.Session{
		SessionID:    sessionID,
		Slug:         slug,
		CWD:          cwd,
		ProjectName:  projectName,
		JSONLPath:    jsonlPath,
		State:        st,
		LastActivity: lastActivity,
		Activities:   activities,
		LastText:     motivation,
		PID:          pid,
		TTY:          tty,
		GitBranch:    gitBranch,
		PendingTools: pendingTools,
	}
}

// RunLoop runs the scanner on a timer, updating the state manager.
func RunLoop(sc *Scanner, st interface {
	UpdateSessionFromScanner(s *model.Session)
}, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial scan.
	scan(sc, st)

	for {
		select {
		case <-ticker.C:
			scan(sc, st)
		case <-stop:
			return
		}
	}
}

func scan(sc *Scanner, st interface{ UpdateSessionFromScanner(s *model.Session) }) {
	sessions := sc.Discover()
	for _, s := range sessions {
		st.UpdateSessionFromScanner(s)
	}
	if len(sessions) > 0 {
		log.Printf("scanner: found %d sessions", len(sessions))
	}
}
