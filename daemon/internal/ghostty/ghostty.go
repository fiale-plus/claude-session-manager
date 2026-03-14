// Package ghostty provides AppleScript-based Ghostty tab correlation.
// Used only to enrich session data with tab names — NOT for approvals.
package ghostty

import (
	"fmt"
	"log"
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

// SwitchToTab switches to a Ghostty tab by name. Returns true on success.
func SwitchToTab(tabName string) bool {
	tabs := GetTabs()
	for _, tab := range tabs {
		if tab.Name == tabName {
			script := fmt.Sprintf(`tell application "Ghostty"
    activate
    set w to front window
    set tabList to every tab of w
    repeat with t in tabList
        if name of t is "%s" then
            set selected of t to true
            return true
        end if
    end repeat
    return false
end tell`, strings.ReplaceAll(tabName, `"`, `\"`))
			_, err := runOsascript(script)
			return err == nil
		}
	}
	return false
}

// SendApproval sends a "y" keystroke to the named Ghostty tab, then switches back.
func SendApproval(tabName string) bool {
	return sendKeystroke(tabName, "y")
}

// SendRejection sends an "n" keystroke to the named Ghostty tab, then switches back.
func SendRejection(tabName string) bool {
	return sendKeystroke(tabName, "n")
}

func sendKeystroke(tabName, key string) bool {
	// Find which tab is currently selected so we can switch back.
	tabs := GetTabs()
	var originalTab string
	for _, t := range tabs {
		if t.Selected {
			originalTab = t.Name
			break
		}
	}

	if !SwitchToTab(tabName) {
		return false
	}

	script := fmt.Sprintf(`tell application "System Events"
    delay 0.1
    keystroke "%s"
end tell`, key)
	_, err := runOsascript(script)
	if err != nil {
		return false
	}

	// Switch back to the original tab.
	if originalTab != "" && originalTab != tabName {
		SwitchToTab(originalTab)
	}
	return true
}

func runOsascript(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
