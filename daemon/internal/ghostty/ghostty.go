// Package ghostty provides AppleScript-based Ghostty tab correlation.
// Used only to enrich session data with tab names — NOT for approvals.
package ghostty

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 10 * time.Second

// Tab represents a Ghostty terminal tab.
type Tab struct {
	ID               string
	Name             string
	Index            int
	Selected         bool
	WorkingDirectory string
}

var (
	tabCache     []Tab
	tabCacheTime time.Time
	cacheMu      sync.Mutex
)

const enumerateScript = `tell application "Ghostty"
    set w to front window
    set tabList to every tab of w
    set output to ""
    repeat with t in tabList
        set tProps to properties of t
        set tId to id of tProps
        set tName to name of tProps
        set tIndex to index of tProps
        set tSelected to selected of tProps
        set term to focused terminal of t
        set termProps to properties of term
        set tDir to working directory of termProps
        set output to output & tId & "\t" & tName & "\t" & tIndex & "\t" & tSelected & "\t" & tDir & "\n"
    end repeat
    return output
end tell`

// GetTabs returns all tabs from the frontmost Ghostty window.
// Results are cached for cacheTTL.
func GetTabs() []Tab {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if tabCache != nil && time.Since(tabCacheTime) < cacheTTL {
		return tabCache
	}

	raw, err := runOsascript(enumerateScript)
	if err != nil {
		log.Printf("ghostty: %v", err)
		return nil
	}

	var tabs []Tab
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		idx, _ := strconv.Atoi(parts[2])
		tabs = append(tabs, Tab{
			ID:               parts[0],
			Name:             parts[1],
			Index:            idx,
			Selected:         strings.ToLower(parts[3]) == "true",
			WorkingDirectory: parts[4],
		})
	}

	tabCache = tabs
	tabCacheTime = time.Now()
	return tabs
}

// CorrelateTab finds the Ghostty tab for a given working directory.
// Also matches if session CWD is a subdirectory of a tab's working directory.
func CorrelateTab(cwd string) string {
	cwd = strings.TrimRight(cwd, "/")
	// First pass: exact match.
	for _, tab := range GetTabs() {
		tabDir := strings.TrimRight(tab.WorkingDirectory, "/")
		if tabDir == cwd {
			return tab.Name
		}
	}
	// Second pass: subdirectory match (session cwd is child of tab dir).
	for _, tab := range GetTabs() {
		tabDir := strings.TrimRight(tab.WorkingDirectory, "/")
		if strings.HasPrefix(cwd, tabDir+"/") {
			return tab.Name
		}
	}
	return ""
}

// SwitchToTab switches to a Ghostty tab by name via System Events.
// Requires Accessibility permissions for csm-daemon.
func SwitchToTab(tabName string) bool {
	safeName := strings.ReplaceAll(strings.ReplaceAll(tabName, `\`, `\\`), `"`, `\"`)
	script := fmt.Sprintf(`tell application "System Events" to tell process "Ghostty"
    click radio button "%s" of tab group 1 of window 1
end tell`, safeName)
	_, err := runOsascript(script)
	return err == nil
}

// SendApprovalToTTY writes "y\n" directly to a TTY device.
// No AppleScript, no Accessibility permissions needed.
func SendApprovalToTTY(tty string) bool {
	return writeTTY(tty, "y\n")
}

// SendRejectionToTTY writes "n\n" directly to a TTY device.
func SendRejectionToTTY(tty string) bool {
	return writeTTY(tty, "n\n")
}

func writeTTY(tty, text string) bool {
	dev := tty
	if !strings.HasPrefix(dev, "/dev/") {
		dev = "/dev/" + tty
	}
	f, err := os.OpenFile(dev, os.O_WRONLY, 0)
	if err != nil {
		log.Printf("ghostty: cannot open TTY %s: %v", dev, err)
		return false
	}
	defer f.Close()
	_, err = f.WriteString(text)
	if err != nil {
		log.Printf("ghostty: cannot write to TTY %s: %v", dev, err)
		return false
	}
	return true
}

// SendApproval sends 'y' to a session — tries TTY first, falls back to AppleScript.
func SendApproval(tabName string) bool {
	return sendKeystroke(tabName, "y")
}

// SendRejection sends 'n' to a session — tries TTY first, falls back to AppleScript.
func SendRejection(tabName string) bool {
	return sendKeystroke(tabName, "n")
}

func sendKeystroke(tabName, key string) bool {
	// AppleScript fallback (needs Accessibility).
	safeName := strings.ReplaceAll(strings.ReplaceAll(tabName, `\`, `\\`), `"`, `\"`)
	tabs := GetTabs()
	var originalTab string
	for _, t := range tabs {
		if t.Selected {
			originalTab = t.Name
			break
		}
	}
	lines := []string{
		`tell application "System Events" to tell process "Ghostty"`,
		fmt.Sprintf(`    click radio button "%s" of tab group 1 of window 1`, safeName),
		`    delay 0.15`,
		fmt.Sprintf(`    keystroke "%s"`, key),
		`    keystroke return`,
	}
	if originalTab != "" && originalTab != tabName {
		safeOrig := strings.ReplaceAll(strings.ReplaceAll(originalTab, `\`, `\\`), `"`, `\"`)
		lines = append(lines, `    delay 0.15`)
		lines = append(lines, fmt.Sprintf(`    click radio button "%s" of tab group 1 of window 1`, safeOrig))
	}
	lines = append(lines, `end tell`)
	_, err := runOsascript(strings.Join(lines, "\n"))
	return err == nil
}

func runOsascript(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil && strings.Contains(string(out), "assistive access") {
		// Fallback: write script to temp file and run via osascript <file>.
		// This avoids shell quoting issues and may use different Accessibility context.
		log.Printf("ghostty: osascript -e failed (assistive access), trying via temp file")
		f, ferr := os.CreateTemp("", "csm-*.scpt")
		if ferr != nil {
			return "", err
		}
		tmpPath := f.Name()
		defer os.Remove(tmpPath)
		f.WriteString(script)
		f.Close()
		out2, err2 := exec.Command("osascript", tmpPath).CombinedOutput()
		if err2 != nil {
			log.Printf("ghostty: osascript file also failed: %v, output: %s", err2, strings.TrimSpace(string(out2)))
			return "", err2
		}
		return strings.TrimSpace(string(out2)), nil
	}
	if err != nil {
		log.Printf("ghostty: osascript error: %v, output: %s", err, strings.TrimSpace(string(out)))
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
