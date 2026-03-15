// Package ghostty provides AppleScript-based Ghostty tab correlation
// and tab switching for the "focus" (Enter) action.
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
// Returns (tab name, tab index). Index is 1-based.
func CorrelateTab(cwd string) (string, int) {
	cwd = strings.TrimRight(cwd, "/")
	tabs := GetTabs()
	// Exact match first.
	for _, tab := range tabs {
		tabDir := strings.TrimRight(tab.WorkingDirectory, "/")
		if tabDir == cwd {
			return tab.Name, tab.Index
		}
	}
	// Subdirectory match.
	for _, tab := range tabs {
		tabDir := strings.TrimRight(tab.WorkingDirectory, "/")
		if strings.HasPrefix(cwd, tabDir+"/") {
			return tab.Name, tab.Index
		}
	}
	return "", 0
}

// SwitchToTabByIndex switches to a Ghostty tab by its 1-based index.
// Uses index instead of name because tab names have animated spinner prefixes.
func SwitchToTabByIndex(index int) bool {
	script := fmt.Sprintf(`tell application "System Events" to tell process "Ghostty"
    set tabGroup to tab group 1 of window 1
    set allButtons to every radio button of tabGroup
    if (count of allButtons) >= %d then
        click item %d of allButtons
        return true
    end if
    return false
end tell`, index, index)
	_, err := runOsascript(script)
	return err == nil
}

func runOsascript(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		log.Printf("ghostty: osascript error: %v, output: %.100s", err, strings.TrimSpace(string(out)))
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
