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

		session := sessionFromJSONL(jsonlPath, proc.pid)
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
			session := sessionFromJSONL(jsonlPath, 0)
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
}

// isClaudeCLI returns true if cmd looks like the actual `claude` CLI binary,
// excluding Claude.app, csm-daemon, and claude-session-manager.
func isClaudeCLI(cmd string) bool {
	base := filepath.Base(cmd)
	if base != "claude" {
		return false
	}
	if strings.Contains(cmd, "Claude.app") {
		return false
	}
	if strings.Contains(cmd, "csm-daemon") || strings.Contains(cmd, "claude-session-manager") {
		return false
	}
	return true
}

func findClaudeProcesses() []procInfo {
	// Use ps to find Claude CLI processes (avoids cgo dependency on process libs).
	out, err := exec.Command("ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil
	}

	type candidate struct {
		pid       int
		sessionID string
	}
	var candidates []candidate

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil {
			continue
		}
		// Extract the command portion after the PID.
		rest := strings.TrimSpace(line[strings.Index(line, " ")+1:])
		// Get the executable (first token).
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			continue
		}
		if !isClaudeCLI(parts[0]) {
			continue
		}
		// Extract session ID from --resume flag.
		var sid string
		for i, arg := range parts {
			if arg == "--resume" && i+1 < len(parts) {
				sid = parts[i+1]
				break
			}
			if strings.HasPrefix(arg, "--resume=") {
				sid = strings.TrimPrefix(arg, "--resume=")
				break
			}
		}
		candidates = append(candidates, candidate{pid: pid, sessionID: sid})
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
		result = append(result, procInfo{pid: c.pid, cwd: cwd, sessionID: c.sessionID})
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

func sessionFromJSONL(jsonlPath string, pid int) *model.Session {
	entries, err := ReadTail(jsonlPath, 50)
	if err != nil || len(entries) == 0 {
		return nil
	}

	sessionID, slug, cwd, gitBranch := ExtractMetadata(entries)
	if sessionID == "" {
		sessionID = filepath.Base(strings.TrimSuffix(jsonlPath, ".jsonl"))
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

	// Only extract pending tools when session is actually running (waiting for approval).
	// In waiting/idle/dead states, any unmatched tool_use blocks are stale history
	// where the tool_result fell outside our tail window.
	var pendingTools []model.PendingTool
	if st == model.StateRunning {
		pendingTools = ExtractPendingTools(entries)
	}

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
