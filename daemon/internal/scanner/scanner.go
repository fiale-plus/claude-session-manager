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
	cwdToPID := make(map[string]int)
	for _, p := range procs {
		if existing, ok := cwdToPID[p.cwd]; !ok || p.pid > existing {
			cwdToPID[p.cwd] = p.pid
		}
	}

	for cwd, pid := range cwdToPID {
		encoded := encodeProjectPath(cwd)
		projectDir := filepath.Join(s.claudeProjectsDir, encoded)
		jsonlPath := findLatestJSONL(projectDir)
		if jsonlPath == "" {
			continue
		}
		if seenJSONL[jsonlPath] {
			continue
		}

		session := sessionFromJSONL(jsonlPath, pid)
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
	pid int
	cwd string
}

func findClaudeProcesses() []procInfo {
	// Use ps to find Claude CLI processes (avoids cgo dependency on process libs).
	out, err := exec.Command("ps", "-eo", "pid,command").Output()
	if err != nil {
		return nil
	}

	var candidates []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(line), "claude") {
			continue
		}
		if strings.Contains(line, "claude-session-manager") || strings.Contains(line, "csm-daemon") {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil {
			continue
		}
		candidates = append(candidates, pid)
	}

	var result []procInfo
	for _, pid := range candidates {
		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err != nil {
			// macOS: use lsof -p <pid> -Fn to get cwd.
			cwd = getCWDMacOS(pid)
		}
		if cwd == "" {
			continue
		}
		result = append(result, procInfo{pid: pid, cwd: cwd})
	}
	return result
}

func getCWDMacOS(pid int) string {
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", pid), "-Fn", "-d", "cwd").Output()
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
		projectName = filepath.Base(filepath.Dir(jsonlPath))
	}

	var st model.SessionState
	if pid > 0 {
		st = DetectState(entries)
	} else {
		st = model.StateDead
	}

	activities := ExtractActivities(entries, 8)
	motivation := ExtractMotivation(entries)
	pendingTools := ExtractPendingTools(entries)

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
